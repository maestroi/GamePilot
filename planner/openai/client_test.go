package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteJSONUsesOpenAICompatibleChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}

		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "test-model" {
			t.Fatalf("model = %q", request.Model)
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
			t.Fatalf("messages = %+v", request.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"rotation\":1,\"target_column\":6}"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/v1", "test-model", "secret")
	got, err := client.CompleteJSON(context.Background(), "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"rotation":1,"target_column":6}` {
		t.Fatalf("content = %q", got)
	}
}

func TestCompleteJSONReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model missing"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/v1", "test-model", "")
	_, err := client.CompleteJSON(context.Background(), "system", "user")
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("error = %v, want HTTP 400", err)
	}
}
