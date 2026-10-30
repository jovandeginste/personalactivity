// Package gitea collects the current user's Gitea activity feed.
package gitea

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/42wim/httpsig"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Source struct{ user string }

func New(user string) Source { return Source{user: user} }
func (Source) Name() string  { return "gitea" }

type feedEvent struct {
	ActUser struct {
		Login string `json:"login"`
	} `json:"act_user"`
	Repo struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
	RefName string    `json:"ref_name"`
	OpType  string    `json:"op_type"`
	Content string    `json:"content"`
	Created time.Time `json:"created"`
}

type commitContent struct {
	Commits []commit `json:"Commits"`
}

type commit struct {
	Sha1      string    `json:"Sha1"`
	Message   string    `json:"Message"`
	Timestamp time.Time `json:"Timestamp"`
}

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	slog.Info("collecting activity", "source", s.Name())
	apiURL := strings.TrimRight(os.Getenv("GITEA_API"), "/")
	if apiURL == "" {
		return nil, fmt.Errorf("GITEA_API is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/users/"+url.PathEscape(s.user)+"/activities/feeds", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := signRequest(req); err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request failed: statuscode %s", resp.Status)
	}

	var feed []feedEvent
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("decode feed: %w", err)
	}
	events := make([]activity.Event, 0, len(feed))
	for _, item := range feed {
		if item.ActUser.Login != s.user {
			continue
		}
		if item.OpType == "commit_repo" {
			commits, err := commitEvents(item)
			if err != nil {
				return nil, err
			}
			events = append(events, commits...)
			continue
		}
		events = append(events, activity.Event{
			Date: item.Created, Source: s.Name(), Topic: item.Repo.FullName,
			Summary:    strings.TrimSpace(strings.Join([]string{item.RefName, item.Content}, " ")),
			Attributes: map[string]string{"op_type": item.OpType, "ref_name": item.RefName, "content": item.Content},
		})
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func commitEvents(item feedEvent) ([]activity.Event, error) {
	var content commitContent
	if err := json.Unmarshal([]byte(item.Content), &content); err != nil {
		return nil, fmt.Errorf("decode commit activity: %w", err)
	}
	events := make([]activity.Event, 0, len(content.Commits))
	for _, commit := range content.Commits {
		events = append(events, activity.Event{
			Date:    commit.Timestamp,
			Source:  "gitea",
			Topic:   item.Repo.FullName,
			Summary: strings.TrimSpace(commit.Message),
			Attributes: map[string]string{
				"op_type":  item.OpType,
				"ref_name": item.RefName,
				"sha1":     commit.Sha1,
			},
		})
	}
	return events, nil
}

func signRequest(req *http.Request) error {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return fmt.Errorf("ssh agent failed: %w", err)
	}
	defer conn.Close()
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return fmt.Errorf("getting signers failed: %w", err)
	}
	signer, cert, principal := findCertSigner(signers)
	if signer == nil {
		return fmt.Errorf("no certificate found in ssh-agent")
	}
	req.Header.Set("x-ssh-certificate", base64.RawStdEncoding.EncodeToString(cert.Marshal()))
	headers := []string{httpsig.RequestTarget, "(created)", "(expires)", "x-ssh-certificate"}
	httpSigner, _, err := httpsig.NewSSHSigner(signer, httpsig.DigestSha512, headers, httpsig.Signature, 10)
	if err != nil {
		return fmt.Errorf("httpsig.NewSSHSigner failed: %w", err)
	}
	return httpSigner.SignRequest(principal+"/certificate", req, nil)
}

func findCertSigner(signers []ssh.Signer) (ssh.Signer, *ssh.Certificate, string) {
	for _, signer := range signers {
		pubKey, err := ssh.ParsePublicKey(signer.PublicKey().Marshal())
		if err != nil {
			continue
		}
		cert, ok := pubKey.(*ssh.Certificate)
		if !ok || time.Until(time.Unix(int64(cert.ValidBefore), 0)) <= 10*time.Second {
			continue
		}
		for _, principal := range cert.ValidPrincipals {
			if strings.HasPrefix(principal, "u") {
				return signer, cert, principal
			}
		}
	}
	return nil, nil, ""
}
