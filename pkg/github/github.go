// Package github collects the current user's GitHub activity feed via the REST API.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
	gogithub "github.com/google/go-github/v93/github"
	"github.com/jovandeginste/personalactivity/pkg/activity"
)

const (
	sourceName = "github"
	attrAction = "action"
	attrPR     = "pull_request"
	attrRef    = "ref"
	attrTitle  = "title"
	attrURL    = "url"
)

type Source struct {
	baseURL string
}

func New() Source           { return Source{baseURL: ""} }
func (Source) Name() string { return sourceName }

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	host, _ := auth.DefaultHost()
	token, _ := auth.TokenForHost(host)

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

		converted, err := convertEvent(item)
		if err != nil {
			return nil, err
		}

		events = append(events, converted...)
	}

	slog.Info("collected activity", "source", s.Name(), "events", len(events))

	return events, nil
}

func (s Source) newClient(host, token string) (*gogithub.Client, error) {
	opts := []gogithub.ClientOptionsFunc{gogithub.WithAuthToken(token)}
	if s.baseURL != "" {
		opts = append(opts, gogithub.WithEnterpriseURLs(s.baseURL, s.baseURL))
	} else if auth.IsEnterprise(host) {
		baseURL := fmt.Sprintf("https://%s/api/v3/", host)
		opts = append(opts, gogithub.WithEnterpriseURLs(baseURL, baseURL))
	}

	client, err := gogithub.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub client: %w", err)
	}

	return client, nil
}

func convertEvent(item *gogithub.Event) ([]activity.Event, error) {
	payload, err := item.ParsePayload()
	if err != nil {
		return nil, fmt.Errorf("decode GitHub event %s (%s): %w", item.GetID(), item.GetType(), err)
	}

	if push, ok := payload.(*gogithub.PushEvent); ok {
		return pushEvents(item, push), nil
	}

	summary, attrs := summarizePayload(item.GetType(), payload)

	return []activity.Event{newEvent(item, summary, attrs)}, nil
}

func pushEvents(item *gogithub.Event, push *gogithub.PushEvent) []activity.Event {
	if len(push.Commits) == 0 {
		summary := fmt.Sprintf("pushed %s to %s", shortSHA(push.GetHead()), push.GetRef())

		return []activity.Event{newEvent(item, summary, map[string]string{
			attrRef:  push.GetRef(),
			"head":   push.GetHead(),
			"before": push.GetBefore(),
		})}
	}

	events := make([]activity.Event, 0, len(push.Commits))
	for _, commit := range push.Commits {
		events = append(events, newEvent(item, commit.GetMessage(), map[string]string{
			attrRef: push.GetRef(),
			"sha":   commit.GetSHA(),
		}))
	}

	return events
}

func summarizePayload(eventType string, payload any) (string, map[string]string) {
	switch p := payload.(type) {
	case *gogithub.CreateEvent:
		return fmt.Sprintf("created %s %s", p.GetRefType(), p.GetRef()),
			map[string]string{"ref_type": p.GetRefType(), attrRef: p.GetRef()}
	case *gogithub.DeleteEvent:
		return fmt.Sprintf("deleted %s %s", p.GetRefType(), p.GetRef()),
			map[string]string{"ref_type": p.GetRefType(), attrRef: p.GetRef()}
	case *gogithub.IssuesEvent:
		issue := p.GetIssue()

		return fmt.Sprintf("%s issue #%d: %s", p.GetAction(), issue.GetNumber(), issue.GetTitle()),
			map[string]string{attrAction: p.GetAction(), "issue": strconv.Itoa(issue.GetNumber()), attrTitle: issue.GetTitle(), attrURL: issue.GetHTMLURL()}
	case *gogithub.IssueCommentEvent:
		issue := p.GetIssue()
		comment := p.GetComment()

		return withBody(fmt.Sprintf("commented on #%d %s", issue.GetNumber(), issue.GetTitle()), comment.GetBody()),
			map[string]string{attrAction: p.GetAction(), "issue": strconv.Itoa(issue.GetNumber()), attrTitle: issue.GetTitle(), attrURL: comment.GetHTMLURL()}
	case *gogithub.PullRequestEvent:
		return summarizePullRequest(p)
	case *gogithub.PullRequestReviewEvent:
		pr := p.GetPullRequest()
		review := p.GetReview()

		return withBody(fmt.Sprintf("reviewed pull request #%d (%s)", pr.GetNumber(), review.GetState()), review.GetBody()),
			map[string]string{attrAction: p.GetAction(), "state": review.GetState(), attrPR: strconv.Itoa(pr.GetNumber()), attrURL: review.GetHTMLURL()}
	case *gogithub.PullRequestReviewCommentEvent:
		pr := p.GetPullRequest()
		comment := p.GetComment()

		return withBody(fmt.Sprintf("commented on pull request #%d", pr.GetNumber()), comment.GetBody()),
			map[string]string{attrAction: p.GetAction(), attrPR: strconv.Itoa(pr.GetNumber()), "path": comment.GetPath(), attrURL: comment.GetHTMLURL()}
	case *gogithub.ReleaseEvent:
		release := p.GetRelease()

		return fmt.Sprintf("%s release %s", p.GetAction(), release.GetTagName()),
			map[string]string{attrAction: p.GetAction(), "tag": release.GetTagName(), "name": release.GetName(), attrURL: release.GetHTMLURL()}
	case *gogithub.ForkEvent:
		forkee := p.GetForkee()

		return fmt.Sprintf("forked to %s", forkee.GetFullName()),
			map[string]string{"forkee": forkee.GetFullName(), attrURL: forkee.GetHTMLURL()}
	case *gogithub.WatchEvent:
		return "starred repository", map[string]string{attrAction: p.GetAction()}
	case *gogithub.CommitCommentEvent:
		comment := p.GetComment()

		return withBody(fmt.Sprintf("commented on commit %s", shortSHA(comment.GetCommitID())), comment.GetBody()),
			map[string]string{"commit": comment.GetCommitID(), attrURL: comment.GetHTMLURL()}
	case *gogithub.PublicEvent:
		return "made repository public", map[string]string{}
	default:
		return eventType, map[string]string{}
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

func newEvent(item *gogithub.Event, summary string, attrs map[string]string) activity.Event {
	attrs["type"] = item.GetType()

	return activity.Event{
		Date:       item.GetCreatedAt().Time,
		Source:     sourceName,
		Topic:      item.GetRepo().GetName(),
		Summary:    strings.TrimSpace(summary),
		Attributes: attrs,
	}
}

func withBody(prefix, body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return prefix
	}

	return prefix + ": " + trimmed
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}
