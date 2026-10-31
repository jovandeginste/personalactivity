// Package github collects the current user's GitHub activity feed and commits via the REST API.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v93/github"
	"github.com/jovandeginste/personalactivity/pkg/activity"
)

const (
	sourceName = "github"
	attrAction = "action"
	attrBody   = "body"
	attrIssue  = "issue"
	attrPR     = "pull_request"
	attrRef    = "ref"
	attrSHA    = "sha"
	attrTitle  = "title"
	attrType   = "type"
	attrURL    = "url"
)

type Source struct {
	baseURL string
}

func New() Source           { return Source{baseURL: ""} }
func (Source) Name() string { return sourceName }

func (s Source) Collect(ctx context.Context, dateRange activity.Range) ([]activity.Event, error) {
	host, token := resolveAuth(ctx)

	if token == "" {
		slog.Warn("skipping source; no GitHub auth token found", "source", s.Name())

		return []activity.Event{}, nil
	}

	slog.Info("collecting activity", "source", s.Name())

	client, err := s.newClient(host, token)
	if err != nil {
		return nil, err
	}

	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("fetch authenticated GitHub user: %w", err)
	}

	login := user.GetLogin()

	events, seenSHAs, err := collectCommits(ctx, client, login)
	if err != nil {
		return nil, err
	}

	feedEvents, err := collectFeedEvents(ctx, client, login, seenSHAs)
	if err != nil {
		return nil, err
	}

	events = append(events, feedEvents...)
	filtered := dateRange.Filter(events)

	slog.Info("collected activity", "source", s.Name(), "events", len(events), "filtered", len(filtered))

	return filtered, nil
}

func (s Source) newClient(host, token string) (*gogithub.Client, error) {
	opts := []gogithub.ClientOptionsFunc{gogithub.WithAuthToken(token)}
	if s.baseURL != "" {
		opts = append(opts, gogithub.WithEnterpriseURLs(s.baseURL, s.baseURL))
	} else if isEnterprise(host) {
		baseURL := fmt.Sprintf("https://%s/api/v3/", host)
		opts = append(opts, gogithub.WithEnterpriseURLs(baseURL, baseURL))
	}

	client, err := gogithub.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}

	return client, nil
}

func collectCommits(ctx context.Context, client *gogithub.Client, login string) ([]activity.Event, map[string]bool, error) {
	opts := &gogithub.SearchOptions{
		Sort:           "author-date",
		Order:          "desc",
		TextMatch:      false,
		AdvancedSearch: nil,
		SearchType:     "",
		ListOptions:    gogithub.ListOptions{Page: 0, PerPage: 100},
	}

	result, _, err := client.Search.Commits(ctx, "author:"+login, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("search GitHub commits: %w", err)
	}

	events := make([]activity.Event, 0, len(result.Commits))
	seenSHAs := make(map[string]bool, len(result.Commits))

	for _, item := range result.Commits {
		sha := item.GetSHA()
		if sha != "" {
			seenSHAs[sha] = true
		}

		msg := strings.TrimSpace(item.GetCommit().GetMessage())
		events = append(events, activity.Event{
			Date:    commitDate(item.GetCommit()),
			Source:  sourceName,
			Topic:   item.GetRepository().GetFullName(),
			Summary: firstLine(msg),
			Attributes: map[string]string{
				attrType:  "commit",
				attrSHA:   sha,
				"message": msg,
				attrURL:   item.GetHTMLURL(),
			},
		})
	}

	return events, seenSHAs, nil
}

func commitDate(commit *gogithub.Commit) time.Time {
	if date := commit.GetAuthor().GetDate().Time; !date.IsZero() {
		return date
	}

	return commit.GetCommitter().GetDate().Time
}

func collectFeedEvents(ctx context.Context, client *gogithub.Client, login string, seenSHAs map[string]bool) ([]activity.Event, error) {
	opts := &gogithub.ListOptions{Page: 0, PerPage: 100}

	ghEvents, _, err := client.Activity.ListEventsPerformedByUser(ctx, login, false, opts)
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub user events: %w", err)
	}

	events := make([]activity.Event, 0, len(ghEvents))
	for _, item := range ghEvents {
		if actor := item.GetActor().GetLogin(); actor != "" && !strings.EqualFold(actor, login) {
			continue
		}

		converted, err := convertEvent(item, seenSHAs)
		if err != nil {
			return nil, err
		}

		events = append(events, converted...)
	}

	return events, nil
}

func convertEvent(item *gogithub.Event, seenSHAs map[string]bool) ([]activity.Event, error) {
	payload, err := item.ParsePayload()
	if err != nil {
		return nil, fmt.Errorf("decode GitHub event %s (%s): %w", item.GetID(), item.GetType(), err)
	}

	if push, ok := payload.(*gogithub.PushEvent); ok {
		return pushEvents(item, push, seenSHAs), nil
	}

	summary, attrs := summarizePayload(item.GetType(), payload)

	return []activity.Event{newEvent(item, summary, attrs)}, nil
}

func pushEvents(item *gogithub.Event, push *gogithub.PushEvent, seenSHAs map[string]bool) []activity.Event {
	if len(push.Commits) == 0 {
		if head := push.GetHead(); head != "" && seenSHAs[head] {
			return nil
		}

		summary := fmt.Sprintf("pushed %s to %s", shortSHA(push.GetHead()), push.GetRef())

		return []activity.Event{newEvent(item, summary, map[string]string{
			attrRef:  push.GetRef(),
			"head":   push.GetHead(),
			"before": push.GetBefore(),
		})}
	}

	events := make([]activity.Event, 0, len(push.Commits))
	for _, commit := range push.Commits {
		sha := commit.GetSHA()
		if sha != "" && seenSHAs[sha] {
			continue
		}

		if sha != "" {
			seenSHAs[sha] = true
		}

		msg := strings.TrimSpace(commit.GetMessage())
		events = append(events, newEvent(item, firstLine(msg), map[string]string{
			attrRef:   push.GetRef(),
			attrSHA:   sha,
			"message": msg,
		}))
	}

	return events
}

func summarizePayload(eventType string, payload any) (string, map[string]string) {
	switch p := payload.(type) {
	case *gogithub.CreateEvent:
		return fmt.Sprintf("created %s %s", p.GetRefType(), p.GetRef()),
			map[string]string{"ref_type": p.GetRefType(), attrRef: p.GetRef(), "description": p.GetDescription()}
	case *gogithub.DeleteEvent:
		return fmt.Sprintf("deleted %s %s", p.GetRefType(), p.GetRef()),
			map[string]string{"ref_type": p.GetRefType(), attrRef: p.GetRef()}
	case *gogithub.IssuesEvent:
		return summarizeIssue(p)
	case *gogithub.IssueCommentEvent:
		return summarizeIssueComment(p)
	case *gogithub.PullRequestEvent:
		return summarizePullRequest(p)
	case *gogithub.PullRequestReviewEvent:
		return summarizePullRequestReview(p)
	case *gogithub.PullRequestReviewCommentEvent:
		return summarizePullRequestReviewComment(p)
	case *gogithub.PullRequestReviewThreadEvent:
		pr := p.GetPullRequest()

		return fmt.Sprintf("%s review thread on pull request #%d", p.GetAction(), pr.GetNumber()),
			map[string]string{attrAction: p.GetAction(), attrPR: strconv.Itoa(pr.GetNumber())}
	case *gogithub.ReleaseEvent:
		release := p.GetRelease()

		return fmt.Sprintf("%s release %s", p.GetAction(), release.GetTagName()),
			map[string]string{attrAction: p.GetAction(), "tag": release.GetTagName(), "name": release.GetName(), attrURL: release.GetHTMLURL()}
	default:
		return summarizeOtherPayload(eventType, payload)
	}
}

func summarizeOtherPayload(eventType string, payload any) (string, map[string]string) {
	switch p := payload.(type) {
	case *gogithub.ForkEvent:
		forkee := p.GetForkee()

		return fmt.Sprintf("forked to %s", forkee.GetFullName()),
			map[string]string{"forkee": forkee.GetFullName(), attrURL: forkee.GetHTMLURL()}
	case *gogithub.WatchEvent:
		return "starred repository", map[string]string{attrAction: p.GetAction()}
	case *gogithub.CommitCommentEvent:
		comment := p.GetComment()
		body := strings.TrimSpace(comment.GetBody())

		return withBody(fmt.Sprintf("commented on commit %s", shortSHA(comment.GetCommitID())), body),
			map[string]string{"commit": comment.GetCommitID(), attrBody: body, attrURL: comment.GetHTMLURL()}
	case *gogithub.DiscussionEvent:
		disc := p.GetDiscussion()

		return fmt.Sprintf("%s discussion #%d: %s", p.GetAction(), disc.GetNumber(), disc.GetTitle()),
			map[string]string{attrAction: p.GetAction(), "discussion": strconv.Itoa(disc.GetNumber()), attrTitle: disc.GetTitle(), attrURL: disc.GetHTMLURL()}
	case *gogithub.DiscussionCommentEvent:
		disc := p.GetDiscussion()
		comment := p.GetComment()
		body := strings.TrimSpace(comment.GetBody())

		return withBody(fmt.Sprintf("commented on discussion #%d %s", disc.GetNumber(), disc.GetTitle()), body),
			map[string]string{attrAction: p.GetAction(), "discussion": strconv.Itoa(disc.GetNumber()), attrTitle: disc.GetTitle(), attrBody: body, attrURL: comment.GetHTMLURL()}
	case *gogithub.GollumEvent:
		return summarizeGollum(p)
	case *gogithub.MemberEvent:
		return fmt.Sprintf("%s member %s", p.GetAction(), p.GetMember().GetLogin()),
			map[string]string{attrAction: p.GetAction(), "member": p.GetMember().GetLogin()}
	case *gogithub.PublicEvent:
		return "made repository public", map[string]string{}
	default:
		return eventType, map[string]string{}
	}
}

func summarizeIssue(p *gogithub.IssuesEvent) (string, map[string]string) {
	issue := p.GetIssue()

	return fmt.Sprintf("%s issue #%d: %s", p.GetAction(), issue.GetNumber(), issue.GetTitle()), map[string]string{
		attrAction: p.GetAction(),
		attrIssue:  strconv.Itoa(issue.GetNumber()),
		attrTitle:  issue.GetTitle(),
		attrURL:    issue.GetHTMLURL(),
	}
}

func summarizeIssueComment(p *gogithub.IssueCommentEvent) (string, map[string]string) {
	issue := p.GetIssue()
	comment := p.GetComment()
	body := strings.TrimSpace(comment.GetBody())

	return withBody(fmt.Sprintf("commented on #%d %s", issue.GetNumber(), issue.GetTitle()), body), map[string]string{
		attrAction: p.GetAction(),
		attrIssue:  strconv.Itoa(issue.GetNumber()),
		attrTitle:  issue.GetTitle(),
		attrBody:   body,
		attrURL:    comment.GetHTMLURL(),
	}
}

func summarizePullRequest(p *gogithub.PullRequestEvent) (string, map[string]string) {
	pr := p.GetPullRequest()
	num := max(p.GetNumber(), pr.GetNumber())
	title := strings.TrimSpace(pr.GetTitle())

	return withBody(fmt.Sprintf("%s pull request #%d", p.GetAction(), num), title), map[string]string{
		attrAction: p.GetAction(),
		attrPR:     strconv.Itoa(num),
		attrTitle:  title,
		attrRef:    pr.GetHead().GetRef(),
		attrURL:    pr.GetHTMLURL(),
	}
}

func summarizePullRequestReview(p *gogithub.PullRequestReviewEvent) (string, map[string]string) {
	pr := p.GetPullRequest()
	review := p.GetReview()
	body := strings.TrimSpace(review.GetBody())

	return withBody(fmt.Sprintf("reviewed pull request #%d (%s)", pr.GetNumber(), review.GetState()), body), map[string]string{
		attrAction: p.GetAction(),
		"state":    review.GetState(),
		attrPR:     strconv.Itoa(pr.GetNumber()),
		attrBody:   body,
		attrURL:    review.GetHTMLURL(),
	}
}

func summarizePullRequestReviewComment(p *gogithub.PullRequestReviewCommentEvent) (string, map[string]string) {
	pr := p.GetPullRequest()
	comment := p.GetComment()
	body := strings.TrimSpace(comment.GetBody())

	return withBody(fmt.Sprintf("commented on pull request #%d", pr.GetNumber()), body), map[string]string{
		attrAction: p.GetAction(),
		attrPR:     strconv.Itoa(pr.GetNumber()),
		"path":     comment.GetPath(),
		attrBody:   body,
		attrURL:    comment.GetHTMLURL(),
	}
}

func summarizeGollum(p *gogithub.GollumEvent) (string, map[string]string) {
	pages := make([]string, 0, len(p.Pages))
	for _, page := range p.Pages {
		pages = append(pages, strings.TrimSpace(page.GetAction()+" "+page.GetTitle()))
	}

	summary := "updated wiki"
	if len(pages) > 0 {
		summary += ": " + strings.Join(pages, ", ")
	}

	return summary, map[string]string{"pages": strings.Join(pages, ", ")}
}

func newEvent(item *gogithub.Event, summary string, attrs map[string]string) activity.Event {
	attrs[attrType] = item.GetType()

	return activity.Event{
		Date:       item.GetCreatedAt().Time,
		Source:     sourceName,
		Topic:      item.GetRepo().GetName(),
		Summary:    strings.TrimSpace(summary),
		Attributes: attrs,
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")

	return strings.TrimSpace(line)
}

func withBody(prefix, body string) string {
	singleLine := strings.Join(strings.Fields(body), " ")
	if singleLine == "" {
		return prefix
	}

	return prefix + ": " + singleLine
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}
