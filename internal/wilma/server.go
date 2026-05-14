package wilma

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Service struct {
	baseURL  string
	username string
	password string
}

func NewServiceFromEnv() (*Service, error) {
	baseURL := strings.TrimSpace(os.Getenv("WILMA_BASE_URL"))
	username := strings.TrimSpace(os.Getenv("WILMA_USERNAME"))
	password := strings.TrimSpace(os.Getenv("WILMA_PASSWORD"))
	if baseURL == "" || username == "" || password == "" {
		return nil, fmt.Errorf("missing credentials. set WILMA_BASE_URL, WILMA_USERNAME and WILMA_PASSWORD")
	}
	return &Service{baseURL: baseURL, username: username, password: password}, nil
}

func (s *Service) newClient() (*Client, error) {
	return NewClient(s.baseURL, s.username, s.password)
}

func NewMCPServer(svc *Service) *server.MCPServer {
	mcpServer := server.NewMCPServer("Wilma MCP", "0.1.0", server.WithToolCapabilities(false), server.WithRecovery())

	mcpServer.AddTool(
		mcp.NewTool("get_schedule",
			mcp.WithDescription("Get the school schedule for a specific date."),
			mcp.WithString("date_str", mcp.Description("Date to get schedule for. Defaults to today.")),
		),
		svc.getSchedule,
	)
	mcpServer.AddTool(
		mcp.NewTool("get_week_schedule",
			mcp.WithDescription("Get the school schedule for a full week."),
			mcp.WithString("start_date", mcp.Description("Start date for week schedule. Defaults to today.")),
		),
		svc.getWeekSchedule,
	)
	mcpServer.AddTool(
		mcp.NewTool("get_messages",
			mcp.WithDescription("Get list of messages from inbox/sent/archive."),
			mcp.WithString("folder", mcp.Description("Folder: inbox, sent, or archive.")),
			mcp.WithNumber("limit", mcp.Description("Max number of messages to return. Defaults to 20.")),
		),
		svc.getMessages,
	)
	mcpServer.AddTool(
		mcp.NewTool("get_message",
			mcp.WithDescription("Read a specific message with full content."),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("ID of the message to read.")),
		),
		svc.getMessage,
	)
	mcpServer.AddTool(
		mcp.NewTool("set_message_read",
			mcp.WithDescription("Mark a message as read."),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("ID of the message to mark read.")),
		),
		svc.setMessageRead,
	)
	mcpServer.AddTool(
		mcp.NewTool("get_recipients",
			mcp.WithDescription("Get available message recipients."),
		),
		svc.getRecipients,
	)
	mcpServer.AddTool(
		mcp.NewTool("send_message",
			mcp.WithDescription("Send a message to a recipient."),
			mcp.WithString("recipient_id", mcp.Required(), mcp.Description("Recipient ID.")),
			mcp.WithString("subject", mcp.Required(), mcp.Description("Message subject.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Message body.")),
			mcp.WithString("reply_to_id", mcp.Description("Optional message id if this is a reply.")),
		),
		svc.sendMessage,
	)
	mcpServer.AddTool(
		mcp.NewTool("reply_to_message",
			mcp.WithDescription("Reply to an existing message."),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID to reply to.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Reply body.")),
		),
		svc.replyToMessage,
	)

	return mcpServer
}

func (s *Service) getSchedule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dateStr := req.GetString("date_str", "today")
	targetDate, err := ParseDate(dateStr)
	if err != nil {
		return mcp.NewToolResultText(err.Error()), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	schedule, err := client.GetSchedule(targetDate)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	return mcp.NewToolResultText(FormatSchedule(schedule)), nil
}

func (s *Service) getWeekSchedule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dateStr := req.GetString("start_date", "today")
	startDate, err := ParseDate(dateStr)
	if err != nil {
		return mcp.NewToolResultText(err.Error()), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	schedules, err := client.GetWeekSchedule(startDate)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}

	lines := []string{"Weekly Schedule", strings.Repeat("=", 40)}
	for _, schedule := range schedules {
		lines = append(lines, "", FormatSchedule(schedule))
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *Service) getMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	folder := strings.ToLower(req.GetString("folder", "inbox"))
	if folder == "" {
		folder = "inbox"
	}
	limit := req.GetInt("limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if folder != "inbox" && folder != "sent" && folder != "archive" {
		return mcp.NewToolResultText("Invalid folder: " + folder + ". Use 'inbox', 'sent', or 'archive'."), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	messages, err := client.GetMessages(folder, limit)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	if len(messages) == 0 {
		return mcp.NewToolResultText("No messages in " + folder + "."), nil
	}

	lines := []string{fmt.Sprintf("Messages in %s (%d shown):", folder, len(messages)), ""}
	for _, msg := range messages {
		status := "📬"
		if msg.IsRead {
			status = "📖"
		}
		lines = append(lines,
			fmt.Sprintf("%s [%s] %s", status, msg.ID, msg.Subject),
			fmt.Sprintf("   From: %s | %s", msg.Sender, msg.Timestamp.Format("2006-01-02 15:04")),
			"",
		)
	}

	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *Service) getMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := req.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	msg, err := client.GetMessage(messageID)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}

	lines := []string{
		"Subject: " + msg.Subject,
		"From: " + msg.Sender,
		"Date: " + msg.Timestamp.Format("2006-01-02 15:04"),
	}
	if len(msg.Recipients) > 0 {
		lines = append(lines, "To: "+strings.Join(msg.Recipients, ", "))
	}
	if len(msg.Attachments) > 0 {
		lines = append(lines, "Attachments: "+strings.Join(msg.Attachments, ", "))
	}
	lines = append(lines, "", "---", "", msg.Content)
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *Service) setMessageRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := req.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	success, err := client.MarkMessageRead(messageID)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	if success {
		return mcp.NewToolResultText("Message " + messageID + " marked as read."), nil
	}
	return mcp.NewToolResultText("Failed to mark message " + messageID + " as read."), nil
}

func (s *Service) getRecipients(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	recipients, err := client.GetRecipients()
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	if len(recipients) == 0 {
		return mcp.NewToolResultText("No recipients found."), nil
	}
	sort.Slice(recipients, func(i, j int) bool {
		return strings.ToLower(recipients[i].Name) < strings.ToLower(recipients[j].Name)
	})
	lines := []string{"Available Recipients:", ""}
	for _, recipient := range recipients {
		role := ""
		if recipient.Role != "" {
			role = " (" + recipient.Role + ")"
		}
		school := ""
		if recipient.School != "" {
			school = " - " + recipient.School
		}
		lines = append(lines, fmt.Sprintf("  [%s] %s%s%s", recipient.ID, recipient.Name, role, school))
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func (s *Service) sendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	recipientID, err := req.RequireString("recipient_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	subject, err := req.RequireString("subject")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body, err := req.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	replyToID := req.GetString("reply_to_id", "")

	if strings.TrimSpace(recipientID) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(body) == "" {
		return mcp.NewToolResultText("Error: recipient_id, subject and body are required"), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	success, err := client.SendMessage([]string{recipientID}, subject, body, replyToID)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	if success {
		return mcp.NewToolResultText("Message sent successfully to recipient " + recipientID + "."), nil
	}
	return mcp.NewToolResultText("Failed to send message."), nil
}

func (s *Service) replyToMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := req.RequireString("message_id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body, err := req.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(body) == "" {
		return mcp.NewToolResultText("Error: message_id and body are required"), nil
	}

	client, err := s.newClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	success, err := client.ReplyToMessage(messageID, body)
	if err != nil {
		return mcp.NewToolResultText("API error: " + err.Error()), nil
	}
	if success {
		return mcp.NewToolResultText("Reply sent successfully to message " + messageID + "."), nil
	}
	return mcp.NewToolResultText("Failed to send reply."), nil
}

func ParseDate(dateStr string) (time.Time, error) {
	normalized := strings.ToLower(strings.TrimSpace(dateStr))
	if normalized == "" {
		normalized = "today"
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch normalized {
	case "today", "tänään":
		return today, nil
	case "tomorrow", "huomenna":
		return today.AddDate(0, 0, 1), nil
	case "yesterday", "eilen":
		return today.AddDate(0, 0, -1), nil
	}

	weekdays := map[string]time.Weekday{
		"monday":      time.Monday,
		"maanantai":   time.Monday,
		"tuesday":     time.Tuesday,
		"tiistai":     time.Tuesday,
		"wednesday":   time.Wednesday,
		"keskiviikko": time.Wednesday,
		"thursday":    time.Thursday,
		"torstai":     time.Thursday,
		"friday":      time.Friday,
		"perjantai":   time.Friday,
		"saturday":    time.Saturday,
		"lauantai":    time.Saturday,
		"sunday":      time.Sunday,
		"sunnuntai":   time.Sunday,
	}
	if targetWeekday, ok := weekdays[normalized]; ok {
		daysAhead := (int(targetWeekday) - int(today.Weekday()) + 7) % 7
		if daysAhead == 0 {
			daysAhead = 7
		}
		return today.AddDate(0, 0, daysAhead), nil
	}

	for _, layout := range []string{"2006-01-02", "2.1.2006", "2.1.", "01/02/2006", "02/01/2006"} {
		parsed, err := time.ParseInLocation(layout, normalized, now.Location())
		if err != nil {
			continue
		}
		if layout == "2.1." {
			parsed = time.Date(today.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, now.Location())
		}
		return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, now.Location()), nil
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}

func FormatSchedule(schedule DaySchedule) string {
	if len(schedule.Lessons) == 0 {
		return "No classes scheduled for " + schedule.Date.Format("Monday, January 02, 2006")
	}
	lines := []string{"Schedule for " + schedule.Date.Format("Monday, January 02, 2006") + ":", ""}
	for _, lesson := range schedule.Lessons {
		parts := []string{fmt.Sprintf("  %s-%s: %s", lesson.StartTime, lesson.EndTime, lesson.Subject)}
		if lesson.Room != "" {
			parts = append(parts, "(Room: "+lesson.Room+")")
		}
		if lesson.Teacher != "" {
			parts = append(parts, "- "+lesson.Teacher)
		}
		if lesson.Notes != "" {
			parts = append(parts, "["+lesson.Notes+"]")
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return strings.Join(lines, "\n")
}

func ParseIntString(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
