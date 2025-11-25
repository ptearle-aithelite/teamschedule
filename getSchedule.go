package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
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

	// Build the stat page URL
	statPage := baseURL + teamStatCode
	log.Println("Fetching schedule from:", statPage)

	// Create a chromedp context to render the page with JavaScript
	// Use a timeout of 15 seconds for the chromedp operations
	chromedpCtx, chromedpCancel := context.WithTimeout(ctx, 15*time.Duration(1000000000))
	defer chromedpCancel()

	// Create chromedp options with User-Agent and other headers to avoid 403
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(chromedpCtx, opts...)
	defer cancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var htmlContent string
	err = chromedp.Run(browserCtx,
		chromedp.Navigate(statPage),
		// Try multiple wait strategies; first wait for any table element
		chromedp.WaitReady(`table`, chromedp.ByQuery),
		// Get the full page HTML after rendering
		chromedp.OuterHTML(`html`, &htmlContent),
	)
	if err != nil {
		log.Printf("chromedp navigation/wait error: %v (will continue with available content)", err)
		// Try to get HTML anyway in case content is partially loaded
		chromedp.Run(browserCtx,
			chromedp.OuterHTML(`html`, &htmlContent),
		)
		if htmlContent == "" {
			return nil, fmt.Errorf("failed to render page with chromedp: %w", err)
		}
	}

	log.Println("Page rendered successfully, parsing schedule table...")

	// Parse the rendered HTML with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse rendered HTML: %w", err)
	}

	// Debug: count all tables and log their selectors
	tables := doc.Find("table")
	log.Printf("Found %d <table> elements on page", tables.Length())
	cardBodyTables := doc.Find("div.card-body > table")
	log.Printf("Found %d <table> elements within div.card-body", cardBodyTables.Length())
	allTables := doc.Find("table")
	allTables.Each(func(i int, t *goquery.Selection) {
		if i >= 3 {
			return // log first 3 tables only
		}
		rows := t.Find("tbody tr")
		log.Printf("Table %d: %d rows, classes=%s", i, rows.Length(), t.AttrOr("class", ""))
	})

	var schedule []TeamSchedule

	// Define helper functions
	getCellText := func(cells *goquery.Selection, idx int) string {
		if idx < cells.Length() {
			return strings.TrimSpace(cells.Eq(idx).Text())
		}
		return ""
	}

	parseScore := func(scoreStr string) (string, string) {
		scoreStr = strings.TrimSpace(scoreStr)
		// remove leading W/L/T
		if len(scoreStr) > 0 && (scoreStr[0] == 'W' || scoreStr[0] == 'L' || scoreStr[0] == 'T') {
			scoreStr = strings.TrimSpace(scoreStr[1:])
		}
		// find first occurrence of \d+-\d+
		parts := strings.FieldsFunc(scoreStr, func(r rune) bool {
			return !(r >= '0' && r <= '9' || r == '-')
		})
		for _, p := range parts {
			if strings.Contains(p, "-") {
				sp := strings.SplitN(p, "-", 2)
				if len(sp) == 2 {
					sf := strings.TrimSpace(sp[0])
					sa := strings.TrimSpace(sp[1])
					if _, err := strconv.Atoi(sf); err == nil {
						if _, err := strconv.Atoi(sa); err == nil {
							return sf, sa
						}
					}
				}
			}
		}
		return "", ""
	}

	parseDateToYYYYMMDD := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}

		// Split on space to remove time portion if present
		datePart := strings.Fields(s)[0]

		layouts := []string{
			"Jan 2, 2006",
			"January 2, 2006",
			"1/2/2006",
			"01/02/2006",
			"2006-01-02",
			"2006/01/02",
			"1/2/06",
			"Jan 2 2006",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, datePart); err == nil {
				return t.Format("20060102")
			}
		}
		// Fallback: return original trimmed string
		return s
	}

	// Process only the first table
	doc.Find("div.card-body > table").Each(func(tableIdx int, table *goquery.Selection) {
		if tableIdx > 0 {
			return // only process the first table, ignore all others
		}

		// Iterate rows within this table
		rows := table.Find("tbody tr")

		rows.Each(func(rowIdx int, row *goquery.Selection) {
			cells := row.Find("td")
			// Be permissive: require only date and opponent cells. Some future rows
			// may have missing score/attendance (colspan or empty cells).
			if cells.Length() < 2 {
				return // need at least date and opponent
			}

			// Extract cell values (optional fields handled safely)
			date := getCellText(cells, 0)
			// scoreResult and attendance may be absent; getCellText handles bounds
			scoreResult := getCellText(cells, 2)
			attendance := getCellText(cells, 3)

			// Extract opponent - try both link text and cell text
			opponentCell := cells.Eq(1)
			opponentLink := opponentCell.Find("a").Text()
			opponent := strings.TrimSpace(opponentLink)
			if opponent == "" {
				// If no link, get all text from cell
				opponent = strings.TrimSpace(opponentCell.Text())
			}

			// Remove leading "@ " if present
			opponent = strings.TrimSpace(strings.TrimPrefix(opponent, "@ "))
			// Remove any words starting with "#"
			words := strings.Fields(opponent)
			var cleanWords []string
			for _, word := range words {
				if !strings.HasPrefix(word, "#") {
					cleanWords = append(cleanWords, word)
				}
			}
			opponent = strings.Join(cleanWords, " ")

			if date == "" || opponent == "" {
				if strings.Contains(date, "11/26") || strings.Contains(opponent, "Canyon") {
					log.Printf("Filtered out: date='%s', opponent='%s'", date, opponent)
				}
				return // skip rows with no date or opponent
			}

			result := ""
			if len(scoreResult) > 0 {
				result = scoreResult[0:1] // first character as result (W/L/T)
			}
			scoreFor, scoreAgainst := parseScore(scoreResult)

			// Format date
			dateFormatted := parseDateToYYYYMMDD(date)

			schedule = append(schedule, TeamSchedule{
				Date:         dateFormatted,
				Opponent:     opponent,
				Attendance:   attendance,
				Result:       result,
				ScoreFor:     scoreFor,
				ScoreAgainst: scoreAgainst,
			})
		})
	})

	log.Printf("Parsed %d schedule entries", len(schedule))

	// return parsed schedule
	return schedule, nil
}
