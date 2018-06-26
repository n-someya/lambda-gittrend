package main

import (
	"context"
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

// Repo : Git repogitry info
type Repo struct {
	Title       string
	URLStr      string
	Description string
	Count       int
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
	resp, err := http.Get("https://github.com/trending?l=" + language)
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

	doc.Find("ol.repo-list li").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("h3 a").Text())
		description := s.Find("p.col-9").Text()
		description = strings.TrimSpace(description)
		url, _ := s.Find("h3 a").Attr("href")
		url = "https://github.com" + url
		//		fmt.Println("title: ", title)
		//		fmt.Println("URL: ", url)
		//		fmt.Println("description: ", description)
		repos = append(repos, Repo{Title: title, URLStr: url, Description: description, Count: 0})
	})

	return repos, nil
}

func registerFirstAppearedRepo(repos *[]Repo) (err error) {
	ctx := context.Background()
	projectID := os.Getenv(GcpProjectID)
	fmt.Println("GCP PrjID =", projectID)

	// Creates a client.
	client, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return err
	}

	// Sets the kind for the new entity.
	kind := "githubTrend"

	for _, repo := range *repos {
		query := datastore.NewQuery(kind).Filter("URLStr =", repo.URLStr)
		existsRepo, err := client.Count(ctx, query)
		if err != nil {
			log.Printf("Failed to query repo: %v", err)
			return err
		}

		// GitRepo whoese existsRepo have 0 is firstly appeared trend repoditry.
		if existsRepo == 0 {
			fmt.Println(repo.Title, " is ", existsRepo)
			key := datastore.NameKey(kind, repo.Title, nil)
			if _, err = client.Put(ctx, key, &repo); err != nil {
				log.Printf("Failed to save repo: %v", err)
				return err
			}
		}
	}

	return nil
}

//HandleRequest => 1.scrape github trend 2. push to google datastore
func HandleRequest(awsctx context.Context) (bool, error) {
	credentialFilename := os.Getenv(GoogleApplicationCredentials)
	err := getGcpJSONKey(credentialFilename)
	if err != nil {
		return false, err
	}
	repos, err := scrapteGithubTrending("python")
	if err != nil {
		return false, err
	}
	err = registerFirstAppearedRepo(&repos)
	if err != nil {
		return false, err
	}
	err = removeGcpJSONKey(credentialFilename)
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
