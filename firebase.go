package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	scopeMessaging = "https://www.googleapis.com/auth/firebase.messaging"
	scopeDatastore = "https://www.googleapis.com/auth/datastore"

	defaultFirestoreBase = "https://firestore.googleapis.com"
	defaultFCMBase       = "https://fcm.googleapis.com"
)

// firebaseClient talks to FCM HTTP v1 and Firestore REST using a service account.
type firebaseClient struct {
	projectID     string
	http          *http.Client
	firestoreBase string
	fcmBase       string
}

var errFirebaseDisabled = errors.New("firebase is not configured: set FIREBASE_CREDENTIALS_FILE to a service-account JSON path")

func newFirebaseClient(ctx context.Context, credsFile, projectOverride string) (*firebaseClient, error) {
	if credsFile == "" {
		return nil, errFirebaseDisabled
	}

	raw, err := os.ReadFile(credsFile)
	if err != nil {
		return nil, fmt.Errorf("read firebase credentials: %w", err)
	}

	creds, err := google.CredentialsFromJSON(ctx, raw, scopeMessaging, scopeDatastore)
	if err != nil {
		return nil, fmt.Errorf("parse firebase credentials: %w", err)
	}

	projectID := projectOverride
	if projectID == "" {
		projectID = creds.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("firebase project id not found; set FIREBASE_PROJECT_ID")
	}

	hc := oauth2.NewClient(ctx, creds.TokenSource)
	hc.Timeout = 20 * time.Second

	return &firebaseClient{projectID: projectID, http: hc, firestoreBase: defaultFirestoreBase, fcmBase: defaultFCMBase}, nil
}

// tokenRef is one FCM token stored by the app at users/{uid}/fcmTokens/{tokenId}.
type tokenRef struct {
	UID     string
	Token   string
	DocName string // full Firestore resource name, used to delete dead tokens
}

// listTokens reads every users/{uid}/fcmTokens/* document (provider == "fcm").
func (f *firebaseClient) listTokens(ctx context.Context) ([]tokenRef, error) {
	url := fmt.Sprintf("%s/v1/projects/%s/databases/(default)/documents:runQuery", f.firestoreBase, f.projectID)
	body := []byte(`{"structuredQuery":{"from":[{"collectionId":"fcmTokens","allDescendants":true}]}}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("firestore runQuery: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var rows []struct {
		Document *struct {
			Name   string `json:"name"`
			Fields struct {
				Token struct {
					StringValue string `json:"stringValue"`
				} `json:"token"`
				Provider struct {
					StringValue string `json:"stringValue"`
				} `json:"provider"`
			} `json:"fields"`
		} `json:"document"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("firestore runQuery decode: %w", err)
	}

	var out []tokenRef
	for _, r := range rows {
		d := r.Document
		if d == nil || d.Fields.Token.StringValue == "" || d.Fields.Provider.StringValue != "fcm" {
			continue
		}
		// .../documents/users/{uid}/fcmTokens/{tokenId}
		parts := strings.Split(d.Name, "/")
		if len(parts) < 4 || parts[len(parts)-2] != "fcmTokens" || parts[len(parts)-4] != "users" {
			continue
		}
		out = append(out, tokenRef{UID: parts[len(parts)-3], Token: d.Fields.Token.StringValue, DocName: d.Name})
	}

	return out, nil
}

// deleteDoc removes a Firestore document by its full resource name.
func (f *firebaseClient) deleteDoc(ctx context.Context, docName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/v1/%s", f.firestoreBase, docName), nil)
	if err != nil {
		return err
	}

	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("firestore delete: HTTP %d", resp.StatusCode)
	}

	return nil
}

type notification struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	ImageURL string            `json:"imageUrl,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
}

// send delivers one message to one device token. unregistered=true means the token is dead.
func (f *firebaseClient) send(ctx context.Context, token string, n notification) (unregistered bool, err error) {
	notif := map[string]string{"title": n.Title, "body": n.Body}
	if n.ImageURL != "" {
		notif["image"] = n.ImageURL
	}

	msg := map[string]any{
		"token":        token,
		"notification": notif,
		"android": map[string]any{
			"priority":     "HIGH",
			"notification": map[string]string{"channel_id": "default"},
		},
		"apns": map[string]any{
			"payload": map[string]any{"aps": map[string]string{"sound": "default"}},
		},
	}
	if len(n.Data) > 0 {
		msg["data"] = n.Data
	}

	payload, err := json.Marshal(map[string]any{"message": msg})
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("%s/v1/projects/%s/messages:send", f.fcmBase, f.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return false, nil
	}

	var e struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &e)

	unregistered = e.Error.Status == "NOT_FOUND" || e.Error.Status == "UNREGISTERED"
	msgText := e.Error.Message
	if msgText == "" {
		msgText = truncate(string(data), 200)
	}

	return unregistered, fmt.Errorf("fcm HTTP %d %s: %s", resp.StatusCode, e.Error.Status, msgText)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}
