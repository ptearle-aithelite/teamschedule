package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type TeamSchedule struct {
	Date         string
	Opponent     string
	Attendance   string
	Result       string
	ScoreFor     string
	ScoreAgainst string
}

func findStatsCode(ctx context.Context, client *firestore.Client, teamID string, year string) (string, error) {
	// Find the team document by searching the collection group "teams"
	iter := client.CollectionGroup("teams").Documents(ctx)
	var teamDoc *firestore.DocumentSnapshot

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return "", fmt.Errorf("team with document ID %s not found", teamID)
		}
		if err != nil {
			return "", err
		}
		if doc.Ref.ID == teamID {
			teamDoc = doc
			break
		}
	}

	// Iterate the team's "teamStats" subcollection and look for a document
	// whose ID matches the provided year. Return its NCAATeamCode field.
	statsIter := teamDoc.Ref.Collection("teamStats").Documents(ctx)
	for {
		statDoc, err := statsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return "", err
		}

		if statDoc.Ref.ID == year {
			v, err := statDoc.DataAt("NCAATeamCode")
			if err != nil {
				return "", fmt.Errorf("NCAATeamCode not found on %s: %v", statDoc.Ref.Path, err)
			}
			code, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("NCAATeamCode is not a string on %s", statDoc.Ref.Path)
			}
			return code, nil
		}
	}

	return "", fmt.Errorf("teamStats for year %s not found for team %s", year, teamID)
}

/*
	func getSchedule(ctx context.Context, client *firestore.Client, teamStatCode string) ([]TeamSchedule, error) {
		// Lookup the association document for NCAA and get the teamStatsSite base URL
		iter := client.Collection("associations").Where("abbreviation", "==", "NCAA").Limit(1).Documents(ctx)
		assocDoc, err := iter.Next()
		if err == iterator.Done {
			return nil, fmt.Errorf("no association with abbreviation 'NCAA' found")
		}
		if err != nil {
			return nil, err
		}

		v, err := assocDoc.DataAt("teamStatsSite")
		if err != nil {
			return nil, fmt.Errorf("teamStatsSite not found on association %s: %v", assocDoc.Ref.Path, err)
		}
		baseURL, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("teamStatsSite is not a string on %s", assocDoc.Ref.Path)
		}

		// TODO: Use baseURL and teamStatCode to fetch schedule from the remote site.
		// For now return an empty schedule so the caller can proceed with further work.
		_ = baseURL
		_ = teamStatCode

		var schedule []TeamSchedule
		return schedule, nil
	}
*/
func main() {
	if len(os.Args) != 4 {
		log.Fatalf(errors.New("Usage - teamschedule <school name> <team name> <year>").Error())
	}

	// Initialize Firestore client
	ctx := context.Background()

	// Use service account key path from environment variable
	sa := option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))

	// Create Firestore client
	client, err := firestore.NewClient(ctx, os.Getenv("GOOGLE_CLOUD_PROJECT"), sa)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	// The document ID you want to find
	teamDocID := os.Args[2]
	yearDocID := os.Args[3]

	teamStatCode, err := findStatsCode(ctx, client, teamDocID, yearDocID)
	if err != nil {
		log.Fatalf("Error finding stats code document: %v", err)
	}

	theSchedule, err := getSchedule(ctx, client, teamStatCode)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if theSchedule == nil {
		log.Fatalf(errors.New("schedule not found").Error())
	}
	log.Printf("Schedule entries: %d", len(theSchedule))

}
