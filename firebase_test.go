package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newFakeClient(base string) *firebaseClient {
	return &firebaseClient{projectID: "proj", http: http.DefaultClient, firestoreBase: base, fcmBase: base}
}

func TestListTokensParsesFirestoreResponse(t *testing.T) {
	const doc = "projects/proj/databases/(default)/documents/users/uid-1/fcmTokens/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/proj/databases/(default)/documents:runQuery" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[
		 {"document":{"name":"`+doc+`a","fields":{"token":{"stringValue":"tok-a"},"provider":{"stringValue":"fcm"}}}},
		 {"document":{"name":"`+doc+`b","fields":{"token":{"stringValue":"tok-b"},"provider":{"stringValue":"apns"}}}},
		 {"document":{"name":"projects/proj/databases/(default)/documents/users/uid-2/fcmTokens/c","fields":{"token":{"stringValue":"tok-c"},"provider":{"stringValue":"fcm"}}}},
		 {"readTime":"2026-11-15T00:00:00Z"}
		]`)
	}))
	defer srv.Close()

	got, err := newFakeClient(srv.URL).listTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UID != "uid-1" || got[0].Token != "tok-a" || got[1].UID != "uid-2" || got[1].Token != "tok-c" {
		t.Fatalf("unexpected tokens: %+v", got)
	}
}

func TestSendAndDeadTokenDetection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Token        string            `json:"token"`
				Notification map[string]string `json:"notification"`
			} `json:"message"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		switch body.Message.Token {
		case "good":
			if body.Message.Notification["title"] != "Hi" {
				t.Errorf("title not forwarded: %+v", body.Message.Notification)
			}
			_, _ = io.WriteString(w, `{"name":"projects/proj/messages/1"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"status":"NOT_FOUND","message":"Requested entity was not found."}}`)
		}
	}))
	defer srv.Close()

	f := newFakeClient(srv.URL)
	if dead, err := f.send(context.Background(), "good", notification{Title: "Hi", Body: "x"}); err != nil || dead {
		t.Fatalf("good token: dead=%v err=%v", dead, err)
	}

	dead, err := f.send(context.Background(), "stale", notification{Title: "Hi"})
	if err == nil || !dead || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("stale token: dead=%v err=%v", dead, err)
	}
}

func TestDeleteDoc(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	if err := newFakeClient(srv.URL).deleteDoc(context.Background(), "projects/proj/databases/(default)/documents/users/u/fcmTokens/t"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/projects/proj/databases/(default)/documents/users/u/fcmTokens/t" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
}
