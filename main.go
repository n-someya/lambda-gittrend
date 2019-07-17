package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/datastore"
	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

// GcpProjectID is env var name which has gcp project id.
const GcpProjectID string = "GCP_PROJECT_ID"

// GoogleApplicationCredentials is env var name which gcp service account key json file path
const GoogleApplicationCredentials string = "GOOGLE_APPLICATION_CREDENTIALS"

// SlackURL is env var name for slack incomming hook
const SlackURL string = "SLACK_URL"

// SlackAttachment is attachemnts of incoming webhook payload
type SlackAttachment struct {
	PreText   string `json:"pretext"`
	Title     string `json:"title"`
	TitleLink string `json:"title_link"`
	Text      string `json:"text"`
	Color     string `json:"color"`
}

// SlackPayload is incoming webhook payload
type SlackPayload struct {
	Text        string            `json:"text"`
	Username    string            `json:"username"`
	IconEmoji   string            `json:"icon_emoji"`
	IconURL     string            `json:"icon_url"`
	Channel     string            `json:"channel"`
	Attachments []SlackAttachment `json:"attachments"`
}

// Repo : Git repogitry info
type Repo struct {
	Title       string
	URLStr      string
	Description string
	Count       int
	Language    string
}

func getGcpJSONKey(credentialFilename string) (err error) {
	//サービスアカウントの認証情報ファイルが存在しなかったら
	if _, err := os.Stat(credentialFilename); os.IsNotExist(err) {
		svc := secretsmanager.New(session.Must(session.NewSession()))
		//	AWS Secrets Manager から json key file(のコンテンツを)DLする
		result, err := svc.GetSecretValue(
			&secretsmanager.GetSecretValueInput{
				SecretId:     aws.String("GCPSecret"),
				VersionStage: aws.String("AWSCURRENT"),
			})
		if err != nil {
			return fmt.Errorf("err: %v", err)
		}
		//  DLしたコンテンツを、GOOGLE_APPLICATION_CREDENTIALSのパス先にファイルとして配備する
		f, err := os.Create(credentialFilename)
		if err != nil {
			return fmt.Errorf("err: %v", err)
		}
		defer f.Close()
		_, err = f.WriteString(*result.SecretString)
		if err != nil {
			return fmt.Errorf("err: %v", err)
		}
		err = f.Sync()
		if err != nil {
			return fmt.Errorf("err: %v", err)
		}
	}
	return nil
}

func removeGcpJSONKey(credentialFilename string) (err error) {
	return os.Remove(credentialFilename)
}
func scrapteGithubTrending(language string) (repos []Repo, err error) {
	resp, err := http.Get("https://github.com/trending/" + language + "?since=daily")
	if err != nil {
		log.Printf("Failed to fetch github repo")
		return repos, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("Http error code = %d, status = %s", resp.StatusCode, resp.Status)
		return repos, fmt.Errorf("Http error code = %d, status = %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Failed to parse github repo")
		return repos, err
	}

	doc.Find("article.Box-row").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("h1 a").Text())
		description := s.Find("p.col-9").Text()
		description = strings.TrimSpace(description)
		url, _ := s.Find("h1 a").Attr("href")
		url = "https://github.com" + url
		//		fmt.Println("title: ", title)
		//		fmt.Println("URL: ", url)
		//		fmt.Println("description: ", description)
		repos = append(repos, Repo{Title: title, URLStr: url, Description: description, Count: 0, Language: language})
	})

	return repos, nil
}

func registerFirstAppearedRepo(repos *[]Repo) (newRepos *[]Repo, err error) {
	ctx := context.Background()
	projectID := os.Getenv(GcpProjectID)
	newAppearedRepos := make([]Repo, 0)

	// Creates a client.
	client, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return nil, err
	}

	// Sets the kind for the new entity.
	kind := "githubTrend"

	for _, repo := range *repos {
		query := datastore.NewQuery(kind).Filter("URLStr =", repo.URLStr)
		existsRepo, err := client.Count(ctx, query)
		if err != nil {
			log.Printf("Failed to query repo: %v", err)
			return nil, err
		}

		// GitRepo whoese existsRepo have 0 is firstly appeared trend repoditry.
		if existsRepo == 0 {
			log.Println(repo.Title, " is ", existsRepo)
			key := datastore.NameKey(kind, repo.Title, nil)
			if _, err = client.Put(ctx, key, &repo); err != nil {
				log.Printf("Failed to save repo: %v", err)
				return nil, err
			}
			newAppearedRepos = append(newAppearedRepos, repo)
		}
	}

	return &newAppearedRepos, nil
}

func sendNewAppearedRepos(repos *[]Repo) (err error) {
	// get slack url from env var
	slackURL := os.Getenv(SlackURL)
	errCount := 0
	for _, repo := range *repos {
		if err := sendNewAppearedRepo(slackURL, repo); err != nil {
			log.Printf("Failed to send %v to %s. Because of %v", repo, slackURL, err)
			errCount++
			continue
		}
	}
	if errCount != 0 {
		return fmt.Errorf("Failed to send repo to slack")
	}
	return nil
}

func sendNewAppearedRepo(slackURL string, repo Repo) (err error) {
	params, err := json.Marshal(
		SlackPayload{
			IconURL: "https://assets-cdn.github.com/images/modules/logos_page/GitHub-Mark.png",
			Attachments: []SlackAttachment{
				SlackAttachment{
					PreText:   repo.Language,
					Title:     repo.Title,
					TitleLink: repo.URLStr,
					Text:      repo.Description,
				},
			},
		})
	if err != nil {
		log.Printf("Can not parse json: %v", err)
		return err
	}
	resp, err := http.Post(
		slackURL,
		"application/json",
		bytes.NewReader(params),
	)
	if err != nil {
		log.Print(err)
		return err
	}
	defer resp.Body.Close()
	return nil
}

//HandleRequest => 1.scrape github trend 2. push to google datastore
func HandleRequest(awsctx context.Context) (bool, error) {
	credentialFilename := os.Getenv(GoogleApplicationCredentials)
	err := getGcpJSONKey(credentialFilename)
	if err != nil {
		return false, err
	}
	// when this func ends, removeGcpJSONKey must be called.
	defer removeGcpJSONKey(credentialFilename)
	repos, err := scrapteGithubTrending("python")
	if err != nil {
		return false, err
	}
	newRepos, err := registerFirstAppearedRepo(&repos)
	if err != nil {
		return false, err
	}
	err = sendNewAppearedRepos(newRepos)
	if err != nil {
		return false, err
	}
	return true, err
}

// Please Specify
// GCP_PROJECT_ID
// GOOGLE_APPLICATION_CREDENTIALS
// AWS_REGION (? 要確認)
func main() {
	lambda.Start(HandleRequest)
}
