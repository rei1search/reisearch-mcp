package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// invite_users: batch sign-up invitations. Client method in
// internal/reisearch/invitations.go.

type InviteUsersInput struct {
	Invitations []reisearch.UserInvitation `json:"invitations"`
}

func inviteUsersError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.Status == http.StatusBadRequest && strings.Contains(apiErr.Message, "No valid emails"):
		return fmt.Errorf("%w — NOTHING was sent: every email was rejected (see details). Use bare addresses like 'name@example.com' and check spelling with the user", err)
	case apiErr.Status == http.StatusBadRequest && strings.Contains(apiErr.Message, "Too many"):
		return fmt.Errorf("%w — split the list into calls of at most %d emails (duplicates count)", err, reisearch.MaxInvitationsPerRequest)
	case apiErr.Status == http.StatusBadRequest:
		return fmt.Errorf("%w — nothing was queued; fix the listed fields and retry", err)
	case apiErr.Status == http.StatusBadGateway:
		return fmt.Errorf("%w — none could be queued (temporary). Safe to retry after a short wait", err)
	case apiErr.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited; wait a minute before trying again", err)
	}
	return err
}

func (h *PropertyHandler) InviteUsers(ctx context.Context, req *mcp.CallToolRequest, input InviteUsersInput) (*mcp.CallToolResult, *reisearch.InviteUsersResult, error) {
	if len(input.Invitations) == 0 {
		return nil, nil, fmt.Errorf("invitations is required: at least one person with 'invited_first_name' and 'invited_email'")
	}
	if len(input.Invitations) > reisearch.MaxInvitationsPerRequest {
		return nil, nil, fmt.Errorf("too many people (%d): at most %d per call — split into several calls", len(input.Invitations), reisearch.MaxInvitationsPerRequest)
	}

	invitations := make([]reisearch.UserInvitation, 0, len(input.Invitations))
	emailCount := 0
	for i, inv := range input.Invitations {
		inv.InvitedFirstName = strings.TrimSpace(inv.InvitedFirstName)
		inv.InvitedLastName = strings.TrimSpace(inv.InvitedLastName)
		if inv.InvitedFirstName == "" {
			return nil, nil, fmt.Errorf("invitations[%d]: invited_first_name is required", i)
		}
		if len(inv.InvitedEmail) == 0 {
			return nil, nil, fmt.Errorf("invitations[%d] (%s): invited_email needs at least one address", i, inv.InvitedFirstName)
		}
		for j, e := range inv.InvitedEmail {
			e = strings.TrimSpace(e)
			if e == "" {
				return nil, nil, fmt.Errorf("invitations[%d] (%s): invited_email[%d] is blank — remove it", i, inv.InvitedFirstName, j)
			}
			if strings.ContainsAny(e, "<> ") {
				return nil, nil, fmt.Errorf("invitations[%d] (%s): %q is not a bare address — send just the address, e.g. 'name@example.com'", i, inv.InvitedFirstName, e)
			}
			inv.InvitedEmail[j] = e
		}
		emailCount += len(inv.InvitedEmail)
		phones := []string{}
		for _, p := range inv.InvitedPhone {
			if p = strings.TrimSpace(p); p != "" {
				phones = append(phones, p)
			}
		}
		inv.InvitedPhone = phones
		invitations = append(invitations, inv)
	}
	if emailCount > reisearch.MaxInvitationsPerRequest {
		return nil, nil, fmt.Errorf("too many emails (%d, duplicates included): at most %d per call — split into several calls", emailCount, reisearch.MaxInvitationsPerRequest)
	}

	result, err := h.client.InviteUsers(ctx, TokenFromContext(ctx), invitations)
	if err != nil {
		return nil, nil, inviteUsersError(err)
	}
	return nil, result, nil
}

func registerInvitationTools(server *mcp.Server, h *PropertyHandler) {
	openWorld := true
	mcp.AddTool(server, &mcp.Tool{Name: "invite_users", Description: "Invite people to sign up for ReiSearch: each email receives a sign-up link (valid 15 days) from the current user. This SENDS REAL EMAILS to outside people — CONFIRM the names and email addresses with the user before calling. " +
		"Requires 'invitations': a list of 1–100 people, each { invited_first_name (required), invited_last_name, invited_email (list, at least one bare address like 'ada@example.com'), invited_phone (list, optional) }. Every email becomes its own invitation; at most 100 emails per call, duplicates included — split bigger lists. " +
		"Returns { queued, invalidEmails, failedEmails, note }. 'queued' is NOT the number of emails sent: people who already have a ReiSearch account, or whom this user already invited, are skipped silently, so say 'invitations sent to up to N people', not 'N people invited'. invalidEmails → tell the user and fix them (resending unchanged fails again); failedEmails → retry ONLY those addresses, never the whole batch. " +
		"Errors: 400 'No valid emails' → nothing sent, fix the addresses; 502 → temporary, safe to retry; 429 → wait.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: &openWorld}}, h.InviteUsers)
}
