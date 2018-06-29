package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSendNewAppearedRepoMock(t *testing.T) {
	ts := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO : verify incoming webhook request
			fmt.Fprintf(w, "Hello HTTP Test")
		}))
	defer ts.Close()
	slackURL := ts.URL
	err := sendNewAppearedRepo(slackURL, Repo{Title: "test", Language: "golang", Description: "testdesc", URLStr: "http://test.local", Count: 0})
	if err != nil {
		t.Errorf("Failed to send message. %v", err)
	}
}
func TestSendNewAppearedRepoToSlack(t *testing.T) {
	slackURL := os.Getenv(SlackURL)
	if slackURL == "" {
		t.Skip("SLACK_URL env is not defined. Skip send to slack test.")
	}
	err := sendNewAppearedRepo(slackURL, Repo{Title: "test", Language: "golang", Description: "testdesc", URLStr: "http://test.local", Count: 0})
	if err != nil {
		t.Errorf("Failed to send message. %v", err)
	}
}
func TestHandleRequest(t *testing.T) {
	slackURL := os.Getenv(SlackURL)
	if slackURL == "" {
		t.Skip("SLACK_URL env is not defined. Skip lambda handler test.")
	}
	projectID := os.Getenv(GcpProjectID)
	if projectID == "" {
		t.Skip("GCP_PROJECT_ID env is not defined. Skip lambda handler test.")
	}
	credentialFilename := os.Getenv(GoogleApplicationCredentials)
	if credentialFilename == "" {
		t.Skip("GCP_APPLICATION_CREDENTIALS env is not defined. Skip lambda handler test.")
	}

	ctx := context.TODO()
	result, err := HandleRequest(ctx)
	if err != nil {
		t.Errorf("Failed to send message. %v", err)
	}

	if result != true {
		t.Errorf("Failed to . %v", err)
	}
}
