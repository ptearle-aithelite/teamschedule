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
	var teamDocRef *firestore.DocumentRef

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return "", fmt.Errorf("team with document ID %s not found", teamID)
		}
		if err != nil {
			return "", err
		}
		if doc.Ref.ID == teamID {
			teamDocRef = doc.Ref
			break
		}
	}

	// Access the year collection under the team doc and get the yearData document
	yearDataDoc, err := teamDocRef.Collection(year).Doc("yearData").Get(ctx)
	if err != nil {
		return "", fmt.Errorf("yearData document not found in collection %s for team %s: %v", year, teamID, err)
	}

	// Extract NCAATeamStatsCode field
	v, err := yearDataDoc.DataAt("NCAATeamStatsCode")
	if err != nil {
		return "", fmt.Errorf("NCAATeamStatsCode not found in %s: %v", yearDataDoc.Ref.Path, err)
	}
	code, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("NCAATeamStatsCode is not a string in %s", yearDataDoc.Ref.Path)
	}
	return code, nil
}

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

	// Find the team document to get its full path (contains conference and school info)
	teamIter := client.CollectionGroup("teams").Documents(ctx)
	var teamDocRef *firestore.DocumentRef
	for {
		doc, err := teamIter.Next()
		if err == iterator.Done {
			log.Fatalf("Could not find team document for team %s", teamDocID)
		}
		if err != nil {
			log.Fatalf("Error searching for team: %v", err)
		}
		if doc.Ref.ID == teamDocID {
			teamDocRef = doc.Ref
			log.Printf("Found team at path: %s", teamDocRef.Path)
			break
		}
	}

	// Write schedule entries to the schedule collection under yearData doc
	// Path: teams/{teamID}/{year}/yearData/schedule/{date}
	yearDataDocRef := teamDocRef.Collection(yearDocID).Doc("yearData")
	scheduleCollection := yearDataDocRef.Collection("schedule")

	log.Printf("Writing schedule entries to: %s", scheduleCollection.Path)

	for i, entry := range theSchedule {
		// Use date as document ID (or you could use auto-generated IDs)
		docRef := scheduleCollection.Doc(entry.Date)
		_, err := docRef.Set(ctx, entry)
		if err != nil {
			log.Fatalf("Failed to write schedule entry %d (path: %s): %v", i, docRef.Path, err)
		}
		log.Printf("Wrote schedule entry %d: %s vs %s (path: %s)", i, entry.Date, entry.Opponent, docRef.Path)
	}

	log.Printf("Successfully wrote %d schedule entries", len(theSchedule))
}
