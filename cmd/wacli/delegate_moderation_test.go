package main

import (
	"strings"
	"testing"

	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/wa"
)

func TestMessagesDeleteDelegatesAdminRevokeWhenStoreLocked(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatalf("lock store: %v", err)
	}
	defer lk.Release()

	server := startPresenceDelegateTestSocket(t, storeDir, func(req sendDelegateRequest) sendDelegateResponse {
		return sendDelegateResponse{OK: true, Sent: true, To: "123@g.us", ID: "revoke-id", Target: req.ID}
	})
	defer server.stop()

	stdout, stderr, err := runPresenceDelegateHelper(t, []string{
		"--store", storeDir, "--json", "--timeout", "750ms",
		"messages", "delete", "--chat", "123@g.us", "--id", "ABC", "--sender", "999@lid", "--post-send-wait", "25ms",
	})
	if err != nil {
		t.Fatalf("messages delete failed: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	req := server.nextRequest(t)
	if req.Kind != deleteDelegateKind || req.To != "123@g.us" || req.ID != "ABC" || req.Sender != "999@lid" || req.PostSendWaitMS != 25 {
		t.Fatalf("delegate request = %+v", req)
	}
	for _, want := range []string{`"revoked":true`, `"id":"revoke-id"`, `"target":"ABC"`, `"delegated":true`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout %q missing %s", stdout, want)
		}
	}
}

func TestGroupsParticipantsRemoveDelegatesWhenStoreLocked(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatalf("lock store: %v", err)
	}
	defer lk.Release()

	server := startPresenceDelegateTestSocket(t, storeDir, func(req sendDelegateRequest) sendDelegateResponse {
		return sendDelegateResponse{OK: true, To: req.To, Action: req.ParticipantAction,
			Participants: []byte(`[{"JID":"4912345@s.whatsapp.net","Error":0}]`)}
	})
	defer server.stop()

	stdout, stderr, err := runPresenceDelegateHelper(t, []string{
		"--store", storeDir, "--json", "--timeout", "750ms",
		"groups", "participants", "remove", "--jid", "123@g.us", "--user", "+4912345",
	})
	if err != nil {
		t.Fatalf("participants remove failed: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	req := server.nextRequest(t)
	if req.Kind != participantsDelegateKind || req.To != "123@g.us" || req.ParticipantAction != "remove" ||
		len(req.Users) != 1 || req.Users[0] != "+4912345" {
		t.Fatalf("delegate request = %+v", req)
	}
	if !strings.Contains(stdout, `"success":true`) || !strings.Contains(stdout, "4912345@s.whatsapp.net") {
		t.Fatalf("stdout %q", stdout)
	}
}

func TestMessagesDeleteForMeDoesNotDelegate(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatalf("lock store: %v", err)
	}
	defer lk.Release()
	server := startPresenceDelegateTestSocket(t, storeDir, func(req sendDelegateRequest) sendDelegateResponse {
		return sendDelegateResponse{OK: true}
	})
	defer server.stop()
	_, stderr, err := runPresenceDelegateHelper(t, []string{
		"--store", storeDir, "--json", "--timeout", "750ms",
		"messages", "delete", "--chat", "123@g.us", "--id", "ABC", "--for-me",
	})
	if err == nil || !strings.Contains(stderr, "store is locked") {
		t.Fatalf("for-me delete should fail on locked store, err=%v stderr=%q", err, stderr)
	}
}

func TestValidateParticipantJID(t *testing.T) {
	for in, ok := range map[string]bool{"+4912345": true, "4912345@s.whatsapp.net": true, "123456789@lid": true, "@@@": false, "x@g.us": false} {
		j, err := wa.ParseUserOrJID(in)
		if err == nil {
			err = validateParticipantJID(j)
		}
		if (err == nil) != ok {
			t.Fatalf("%q: err=%v, want ok=%v", in, err, ok)
		}
	}
}
