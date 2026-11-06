package reisearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// User invitations: POST /connect/v1/user-invitations queues one sign-up
// invitation per valid email (202). A background worker creates the
// invitation (valid 15 days) and emails the link; it silently skips emails
// that already belong to a user or that this caller already invited. Uses
// apiDo from underwriting.go.

// MaxInvitationsPerRequest caps both people and emails (emails counted
// before de-duplication), mirroring dto.MaxEmailsPerRequest in the API.
const MaxInvitationsPerRequest = 100

type UserInvitation struct {
	InvitedFirstName string   `json:"invited_first_name"`
	InvitedLastName  string   `json:"invited_last_name,omitempty"`
	InvitedPhone     []string `json:"invited_phone,omitempty"`
	InvitedEmail     []string `json:"invited_email"`
}

type InviteUsersResult struct {
	Queued        int      `json:"queued"`
	InvalidEmails []string `json:"invalidEmails"`
	FailedEmails  []string `json:"failedEmails"`
	Note          string   `json:"note"`
}

func (c *Client) InviteUsers(ctx context.Context, token string, invitations []UserInvitation) (*InviteUsersResult, error) {
	body := map[string]any{"invitations": invitations}
	raw, err := c.apiDo(ctx, token, http.MethodPost, "/connect/v1/user-invitations", url.Values{}, body)
	if err != nil {
		return nil, err
	}
	var out InviteUsersResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.InvalidEmails == nil {
		out.InvalidEmails = []string{}
	}
	if out.FailedEmails == nil {
		out.FailedEmails = []string{}
	}
	out.Note = "queued = invitations handed to the email worker, NOT emails sent: people who already have a ReiSearch account, or whom this user already invited, are skipped silently. Invitations expire after 15 days."
	return &out, nil
}
