package main

// Delegation of `messages delete` (including group admin deletes) and
// `groups participants <action>` to a running `sync --follow` process through
// the send-delegate socket, so moderation writes reuse its WhatsApp connection
// while it holds the store lock.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

const (
	deleteDelegateKind       = "delete"
	participantsDelegateKind = "group_participants"
)

func executeDelegatedDelete(ctx context.Context, a *app.App, req sendDelegateRequest) (sendDelegateResponse, error) {
	msg, chatJID, err := loadMessageMutationTarget(ctx, a, req.To, req.ID)
	if err != nil {
		return sendDelegateResponse{}, err
	}
	revokeSender, err := resolveRevokeSender(msg, true, chatJID, req.Sender)
	if err != nil {
		return sendDelegateResponse{}, err
	}
	if err := warnRapidSendIfNeeded(a.StoreDir(), time.Now().UTC(), os.Stderr); err != nil {
		return sendDelegateResponse{}, err
	}
	sentID, err := runSendOperation(ctx, reconnectForSend(a), func(ctx context.Context) (types.MessageID, error) {
		return a.WA().RevokeMessage(ctx, chatJID, revokeSender, types.MessageID(msg.MsgID))
	})
	if err != nil {
		return sendDelegateResponse{}, err
	}
	resp := sendDelegateResponse{OK: true, Sent: true, To: chatJID.String(), ID: string(sentID), Target: msg.MsgID}
	if err := a.DB().MarkMessageRevoked(msg.ChatJID, msg.MsgID); err != nil {
		resp.StoreWarning = fmt.Sprintf("store deleted message state: %v", err)
	}
	waitForPostSendRetryReceipts(ctx, millisDuration(req.PostSendWaitMS, 0))
	return resp, nil
}

func executeDelegatedGroupParticipants(ctx context.Context, a *app.App, req sendDelegateRequest) (sendDelegateResponse, error) {
	action := strings.TrimSpace(req.ParticipantAction)
	switch action {
	case "add", "remove", "promote", "demote":
	default:
		return sendDelegateResponse{}, fmt.Errorf("unknown participant action %q", action)
	}
	if len(req.Users) == 0 {
		return sendDelegateResponse{}, fmt.Errorf("at least one user is required")
	}
	gjid, err := types.ParseJID(req.To)
	if err != nil {
		return sendDelegateResponse{}, err
	}
	var jids []types.JID
	for _, u := range req.Users {
		j, err := wa.ParseUserOrJID(u)
		if err != nil {
			return sendDelegateResponse{}, err
		}
		if err := validateParticipantJID(j); err != nil {
			return sendDelegateResponse{}, fmt.Errorf("invalid user %q: %w", u, err)
		}
		jids = append(jids, j)
	}
	updated, err := a.WA().UpdateGroupParticipants(ctx, gjid, jids, wa.GroupParticipantAction(action))
	if err != nil {
		return sendDelegateResponse{}, err
	}
	if info, err := a.WA().GetGroupInfo(ctx, gjid); err == nil && info != nil {
		_ = persistGroupInfo(ctx, a.DB(), a.WA(), info)
	}
	raw, err := json.Marshal(updated)
	if err != nil {
		return sendDelegateResponse{}, err
	}
	return sendDelegateResponse{OK: true, To: gjid.String(), Action: action, Participants: raw}, nil
}

// validateParticipantJID rejects malformed user JIDs locally so they never reach WhatsApp.
func validateParticipantJID(j types.JID) error {
	if strings.TrimSpace(j.User) == "" {
		return fmt.Errorf("empty user part")
	}
	if j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer {
		return fmt.Errorf("unsupported server %q (want %s or %s)", j.Server, types.DefaultUserServer, types.HiddenUserServer)
	}
	return nil
}
