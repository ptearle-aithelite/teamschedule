package main

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

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

	// Example of building the stat page URL — actual usage to be implemented.
	statPage := baseURL + teamStatCode
	log.Println("Fetching schedule from: " + statPage)

	// TODO: Fetch and parse schedule from statPage. For now return empty schedule.
	var schedule []TeamSchedule
	return schedule, nil
}
