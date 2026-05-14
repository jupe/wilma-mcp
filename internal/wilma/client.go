package wilma

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	errAuth = errors.New("authentication error")
	errAPI  = errors.New("api error")
)

type Client struct {
	baseURL    *url.URL
	username   string
	password   string
	httpClient *http.Client
	sessionID  string
	userPrefix string
}

type responseData struct {
	StatusCode int
	URL        string
	Body       []byte
}

func NewClient(baseURL, username, password string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	return &Client{
		baseURL:  u,
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}, nil
}

func (c *Client) Login() error {
	indexURL := c.baseURL.ResolveReference(&url.URL{Path: "/index_json"})
	resp, err := c.httpClient.Get(indexURL.String())
	if err != nil {
		return fmt.Errorf("%w: get index_json: %v", errAuth, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: index_json returned %d", errAuth, resp.StatusCode)
	}

	var indexData struct {
		SessionID string `json:"SessionID"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&indexData); err != nil {
		return fmt.Errorf("%w: decode index_json: %v", errAuth, err)
	}
	if indexData.SessionID == "" {
		return fmt.Errorf("%w: no SessionID in index_json", errAuth)
	}

	form := url.Values{}
	form.Set("Login", c.username)
	form.Set("Password", c.password)
	form.Set("SESSIONID", indexData.SessionID)

	loginURL := c.baseURL.ResolveReference(&url.URL{Path: "/login"})
	loginResp, err := c.httpClient.Post(loginURL.String(), "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: login request failed: %v", errAuth, err)
	}
	bodyBytes, _ := io.ReadAll(loginResp.Body)
	_ = loginResp.Body.Close()

	c.sessionID = ""
	for _, cookie := range c.httpClient.Jar.Cookies(c.baseURL) {
		if cookie.Name == "Wilma2SID" {
			c.sessionID = cookie.Value
			break
		}
	}
	if c.sessionID == "" {
		return fmt.Errorf("%w: login failed - no Wilma2SID cookie", errAuth)
	}

	finalURL := ""
	if loginResp.Request != nil && loginResp.Request.URL != nil {
		finalURL = loginResp.Request.URL.String()
	}
	prefixMatch := regexp.MustCompile(`(/!\d+)`).FindStringSubmatch(finalURL)
	if len(prefixMatch) > 1 {
		c.userPrefix = prefixMatch[1]
		return nil
	}

	prefixMatch = regexp.MustCompile(`href="(/!\d+)`).FindStringSubmatch(string(bodyBytes))
	if len(prefixMatch) > 1 {
		c.userPrefix = prefixMatch[1]
		return nil
	}

	return fmt.Errorf("%w: could not determine user prefix", errAuth)
}

func (c *Client) ensureAuthenticated() error {
	if c.sessionID == "" || c.userPrefix == "" {
		return c.Login()
	}
	return nil
}

func (c *Client) request(method, path string, body io.Reader, contentType string) (*responseData, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, err
	}

	requestPath := path
	if !strings.HasPrefix(path, "/!") && !strings.HasPrefix(path, "/preferences") {
		requestPath = c.userPrefix + path
	}

	reqURL := c.baseURL.ResolveReference(&url.URL{Path: requestPath})
	req, err := http.NewRequest(method, reqURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", errAPI, err)
	}
	req.Header.Set("Accept", "application/json, text/html")
	req.Header.Set("User-Agent", "WilmaMCP-Go/0.1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed: %v", errAPI, err)
	}
	data, err := readResponse(resp)
	if err != nil {
		return nil, err
	}

	if strings.Contains(strings.ToLower(data.URL), "/login") {
		c.sessionID = ""
		if err := c.Login(); err != nil {
			return nil, err
		}
		if seeker, ok := body.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
		}
		if reader, ok := body.(*bytes.Reader); ok {
			_, _ = reader.Seek(0, io.SeekStart)
		}
		req, err = http.NewRequest(method, reqURL.String(), body)
		if err != nil {
			return nil, fmt.Errorf("%w: rebuild request: %v", errAPI, err)
		}
		req.Header.Set("Accept", "application/json, text/html")
		req.Header.Set("User-Agent", "WilmaMCP-Go/0.1.0")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: retry request failed: %v", errAPI, err)
		}
		return readResponse(resp)
	}

	return data, nil
}

func readResponse(resp *http.Response) (*responseData, error) {
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response body: %v", errAPI, err)
	}
	finalURL := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return &responseData{StatusCode: resp.StatusCode, URL: finalURL, Body: bodyBytes}, nil
}

func (c *Client) GetSchedule(targetDate time.Time) (DaySchedule, error) {
	dateStr := targetDate.Format("02.01.2006")
	resp, err := c.request(http.MethodGet, "/schedule?date="+url.QueryEscape(dateStr), nil, "")
	if err != nil {
		return DaySchedule{}, err
	}
	return parseDayScheduleFromHTML(string(resp.Body), targetDate), nil
}

func (c *Client) GetWeekSchedule(startDate time.Time) ([]DaySchedule, error) {
	dateStr := startDate.Format("02.01.2006")
	resp, err := c.request(http.MethodGet, "/schedule?date="+url.QueryEscape(dateStr), nil, "")
	if err != nil {
		return nil, err
	}
	return parseWeekScheduleFromHTML(string(resp.Body), startDate), nil
}

func parseDayScheduleFromHTML(html string, targetDate time.Time) DaySchedule {
	events := extractEvents(html)
	targetDateStr := targetDate.Format("02.01.2006")
	lessons := make([]Lesson, 0)
	for _, event := range events {
		if asString(event["Date"]) != targetDateStr {
			continue
		}
		startMins := asInt(event["Start"])
		endMins := asInt(event["End"])
		lesson := Lesson{
			StartTime: minutesToHHMM(startMins),
			EndTime:   minutesToHHMM(endMins),
			Subject:   mapValue(event["Text"], "0"),
			Teacher:   strings.TrimPrefix(mapValue(event["Opet"], "0"), "O: "),
			Notes:     mapValue(event["LongText"], "0"),
		}
		lessons = append(lessons, lesson)
	}
	sort.Slice(lessons, func(i, j int) bool { return lessons[i].StartTime < lessons[j].StartTime })
	return DaySchedule{Date: targetDate, Lessons: lessons}
}

func parseWeekScheduleFromHTML(html string, startDate time.Time) []DaySchedule {
	dayCount := 5
	if m := regexp.MustCompile(`DayCount\s*:\s*(\d+)`).FindStringSubmatch(html); len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
			dayCount = v
		}
	}

	events := extractEvents(html)
	byDate := map[string][]Lesson{}
	for _, event := range events {
		eventDate := asString(event["Date"])
		if eventDate == "" {
			continue
		}
		lesson := Lesson{
			StartTime: minutesToHHMM(asInt(event["Start"])),
			EndTime:   minutesToHHMM(asInt(event["End"])),
			Subject:   mapValue(event["Text"], "0"),
			Teacher:   strings.TrimPrefix(mapValue(event["Opet"], "0"), "O: "),
			Notes:     mapValue(event["LongText"], "0"),
		}
		byDate[eventDate] = append(byDate[eventDate], lesson)
	}

	schedules := make([]DaySchedule, 0, dayCount)
	for i := 0; i < dayCount; i++ {
		day := startDate.AddDate(0, 0, i)
		dayKey := day.Format("02.01.2006")
		lessons := byDate[dayKey]
		sort.Slice(lessons, func(i, j int) bool { return lessons[i].StartTime < lessons[j].StartTime })
		schedules = append(schedules, DaySchedule{Date: day, Lessons: lessons})
	}
	return schedules
}

func extractEvents(html string) []map[string]any {
	start := strings.Index(html, "Events : [")
	if start == -1 {
		start = strings.Index(html, "Events: [")
	}
	if start == -1 {
		return nil
	}
	arrayStart := strings.Index(html[start:], "[")
	if arrayStart == -1 {
		return nil
	}
	arrayStart += start
	count := 0
	arrayEnd := -1
	for i := arrayStart; i < len(html); i++ {
		switch html[i] {
		case '[':
			count++
		case ']':
			count--
			if count == 0 {
				arrayEnd = i + 1
				break
			}
		}
	}
	if arrayEnd == -1 {
		return nil
	}
	jsonBlob := html[arrayStart:arrayEnd]
	var events []map[string]any
	if err := json.Unmarshal([]byte(jsonBlob), &events); err != nil {
		return nil
	}
	return events
}

func minutesToHHMM(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func mapValue(v any, key string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return asString(v)
	}
	return asString(m[key])
}

func (c *Client) GetMessages(folder string, limit int) ([]MessageSummary, error) {
	resp, err := c.request(http.MethodGet, "/messages/list/index_json", nil, "")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Messages []struct {
			ID        any    `json:"Id"`
			Subject   string `json:"Subject"`
			Sender    string `json:"Sender"`
			Timestamp string `json:"TimeStamp"`
			Status    any    `json:"Status"`
			Folder    string `json:"Folder"`
		} `json:"Messages"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("%w: parse messages json: %v", errAPI, err)
	}
	if limit <= 0 || limit > len(payload.Messages) {
		limit = len(payload.Messages)
	}
	messages := make([]MessageSummary, 0, limit)
	for _, msg := range payload.Messages[:limit] {
		timestamp, err := time.Parse("2006-01-02 15:04", msg.Timestamp)
		if err != nil {
			timestamp = time.Now()
		}
		messages = append(messages, MessageSummary{
			ID:        asString(msg.ID),
			Subject:   msg.Subject,
			Sender:    msg.Sender,
			Timestamp: timestamp,
			IsRead:    isFalsy(msg.Status),
			Folder:    firstNonEmpty(msg.Folder, folder),
		})
	}
	return messages, nil
}

func isFalsy(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case float64:
		return t == 0
	case int:
		return t == 0
	case string:
		return t == "" || t == "0" || strings.EqualFold(t, "false")
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (c *Client) GetMessage(messageID string) (Message, error) {
	resp, err := c.request(http.MethodGet, "/messages/"+url.PathEscape(messageID), nil, "")
	if err != nil {
		return Message{}, err
	}
	return parseMessageHTML(string(resp.Body), messageID)
}

func parseMessageHTML(html, messageID string) (Message, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Message{}, fmt.Errorf("%w: parse message html: %v", errAPI, err)
	}

	subject := ""
	if title := strings.TrimSpace(doc.Find("title").First().Text()); title != "" {
		subject = strings.TrimSpace(strings.TrimSuffix(title, " - Wilma"))
	}

	sender := ""
	doc.Find("*:contains('Lähettäjä')").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if next := s.Parent().Next(); next.Length() > 0 {
			sender = strings.TrimSpace(next.Text())
			if sender != "" {
				return false
			}
		}
		return true
	})

	timestamp := time.Now()
	doc.Find("*:contains('Lähetetty')").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if next := s.Parent().Next(); next.Length() > 0 {
			parsed := parseFinnishDateTime(strings.TrimSpace(next.Text()))
			if !parsed.IsZero() {
				timestamp = parsed
				return false
			}
		}
		return true
	})

	content := strings.TrimSpace(doc.Find("div.panel-body").First().Text())
	if content != "" {
		content = regexp.MustCompile(`×\s*Varmistus\s*Jatka\s*Peruuta`).ReplaceAllString(content, "")
		content = strings.TrimSpace(strings.ReplaceAll(content, "Vastaa viestin lähettäjälle", ""))
	}

	return Message{
		ID:        messageID,
		Subject:   subject,
		Sender:    sender,
		Timestamp: timestamp,
		Content:   content,
		IsRead:    true,
	}, nil
}

func parseFinnishDateTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "klo", "")
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		"2.1.2006 15:04",
		"2.1.2006 15.04",
		"2.1.2006",
		"2.1. 15:04",
		"2.1.",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			if strings.Contains(layout, "2006") {
				return t
			}
			return time.Date(time.Now().Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
		}
	}

	m := regexp.MustCompile(`(\d{1,2})\.(\d{1,2})\.(\d{2,4})?\s*(\d{1,2})?[.:]?(\d{2})?`).FindStringSubmatch(raw)
	if len(m) == 0 {
		return time.Time{}
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	year := time.Now().Year()
	if m[3] != "" {
		year, _ = strconv.Atoi(m[3])
		if year < 100 {
			year += 2000
		}
	}
	hour, minute := 0, 0
	if m[4] != "" {
		hour, _ = strconv.Atoi(m[4])
	}
	if m[5] != "" {
		minute, _ = strconv.Atoi(m[5])
	}
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local)
}

func (c *Client) MarkMessageRead(messageID string) (bool, error) {
	resp, err := c.request(http.MethodGet, "/messages/"+url.PathEscape(messageID), nil, "")
	if err != nil {
		return false, err
	}
	return resp.StatusCode == http.StatusOK, nil
}

func (c *Client) GetRecipients() ([]Recipient, error) {
	resp, err := c.request(http.MethodGet, "/messages/compose", nil, "")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body)))
	if err != nil {
		return nil, fmt.Errorf("%w: parse compose html: %v", errAPI, err)
	}
	recipients := make([]Recipient, 0)
	doc.Find("option").Each(func(i int, s *goquery.Selection) {
		value, _ := s.Attr("value")
		value = strings.TrimSpace(value)
		if value == "" || value == "0" {
			return
		}
		name := strings.TrimSpace(s.Text())
		if name == "" {
			return
		}
		role := ""
		if m := regexp.MustCompile(`\(([^)]+)\)$`).FindStringSubmatch(name); len(m) > 1 {
			role = m[1]
			name = strings.TrimSpace(strings.TrimSuffix(name, "("+role+")"))
		}
		recipients = append(recipients, Recipient{ID: value, Name: name, Role: role})
	})
	return recipients, nil
}

func (c *Client) SendMessage(recipientIDs []string, subject, body, replyToID string) (bool, error) {
	composeResp, err := c.request(http.MethodGet, "/messages/compose", nil, "")
	if err != nil {
		return false, err
	}
	formKey := extractFormKey(string(composeResp.Body))
	if formKey == "" {
		return false, fmt.Errorf("%w: missing formkey", errAPI)
	}

	values := url.Values{}
	values.Set("formkey", formKey)
	values.Set("rcpt", strings.Join(recipientIDs, ","))
	values.Set("subject", subject)
	values.Set("body", body)
	if strings.TrimSpace(replyToID) != "" {
		values.Set("replyto", replyToID)
	}

	payload := []byte(values.Encode())
	resp, err := c.request(http.MethodPost, "/messages/compose", bytes.NewReader(payload), "application/x-www-form-urlencoded")
	if err != nil {
		return false, err
	}
	if strings.Contains(strings.ToLower(resp.URL), "messages") {
		return true, nil
	}
	if strings.Contains(strings.ToLower(string(resp.Body)), "error") || strings.Contains(strings.ToLower(string(resp.Body)), "virhe") {
		return false, fmt.Errorf("%w: send message failed with server error", errAPI)
	}
	return false, fmt.Errorf("%w: send message failed with unexpected response", errAPI)
}

func (c *Client) ReplyToMessage(messageID, body string) (bool, error) {
	msgResp, err := c.request(http.MethodGet, "/messages/"+url.PathEscape(messageID), nil, "")
	if err != nil {
		return false, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(msgResp.Body)))
	if err != nil {
		return false, fmt.Errorf("%w: parse message html for reply: %v", errAPI, err)
	}

	replyURL := ""
	doc.Find("a").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, ok := s.Attr("href")
		if !ok {
			return true
		}
		text := strings.TrimSpace(s.Text())
		if strings.Contains(strings.ToLower(text), "vastaa") || regexp.MustCompile(`compose.*(answer|reply)`).MatchString(strings.ToLower(href)) {
			replyURL = href
			return false
		}
		return true
	})
	if replyURL == "" {
		return false, fmt.Errorf("%w: reply link not found", errAPI)
	}

	composeResp, err := c.request(http.MethodGet, replyURL, nil, "")
	if err != nil {
		return false, err
	}
	composeHTML := string(composeResp.Body)
	formKey := extractFormKey(composeHTML)
	if formKey == "" {
		return false, fmt.Errorf("%w: missing formkey in reply form", errAPI)
	}

	replyDoc, err := goquery.NewDocumentFromReader(strings.NewReader(composeHTML))
	if err != nil {
		return false, fmt.Errorf("%w: parse reply compose html: %v", errAPI, err)
	}

	values := url.Values{}
	values.Set("formkey", formKey)
	postURL := "/messages/compose"

	form := replyDoc.Find("form").FilterFunction(func(i int, s *goquery.Selection) bool {
		return s.Find("textarea").Length() > 0
	}).First()

	bodyField := "body"
	if form.Length() > 0 {
		form.Find("input[type='hidden'], input[type='text']").Each(func(i int, s *goquery.Selection) {
			name, nameOk := s.Attr("name")
			value, valueOk := s.Attr("value")
			if nameOk && valueOk && name != "formkey" {
				values.Set(name, value)
			}
		})
		if textarea := form.Find("textarea").First(); textarea.Length() > 0 {
			if name, ok := textarea.Attr("name"); ok && strings.TrimSpace(name) != "" {
				bodyField = name
			}
		}
		if action, ok := form.Attr("action"); ok && strings.HasPrefix(action, "/") && !strings.Contains(strings.ToLower(action), "logout") {
			postURL = action
		}
	}

	values.Set(bodyField, body)
	payload := []byte(values.Encode())
	postResp, err := c.request(http.MethodPost, postURL, bytes.NewReader(payload), "application/x-www-form-urlencoded")
	if err != nil {
		return false, err
	}
	if strings.Contains(strings.ToLower(postResp.URL), "messages") {
		return true, nil
	}
	if strings.Contains(strings.ToLower(string(postResp.Body)), "error") || strings.Contains(strings.ToLower(string(postResp.Body)), "virhe") {
		return false, fmt.Errorf("%w: reply failed with server error", errAPI)
	}
	return false, fmt.Errorf("%w: reply failed with unexpected response", errAPI)
}

func extractFormKey(html string) string {
	for _, pattern := range []string{
		`name="formkey"\s+value="([^"]*)"`,
		`value="([^"]*)"\s+name="formkey"`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
