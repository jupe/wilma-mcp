# Wilma MCP Server

An [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server for [Wilma](https://www.visma.com/wilma) - the Finnish school communication platform by Visma. This allows Claude and other MCP-compatible AI assistants to interact with school data including schedules, messages, and more.

## Features

- **Schedule** - View daily or weekly timetables with subjects, times, and teachers
- **Messages** - Read inbox messages with read/unread status, view full content, mark as read
- **Recipients** - List available message recipients (teachers, staff)
- **Send Messages** - Compose and send messages to teachers

## Prerequisites

- Go 1.25.5 or higher
- A Wilma account (student, guardian, or teacher)
- Your school's Wilma URL (e.g., `https://yourschool.inschool.fi`)

## Installation

```bash
# Clone the repository
git clone https://github.com/jupe/wilma-mcp.git
cd wilma-mcp

# Build binary
go build -o bin/wilma-mcp ./cmd/wilma-mcp
```

## Configuration

Create a `.env` file with your Wilma credentials:

```bash
cp .env.example .env
```

Edit `.env`:

```
WILMA_BASE_URL=https://yourschool.inschool.fi
WILMA_USERNAME=your_username
WILMA_PASSWORD=your_password
```

> **Security Note**: Never commit your `.env` file to version control.

## Usage with Claude Desktop

Add the server to your Claude Desktop configuration file:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "wilma": {
      "command": "/path/to/wilma-mcp/bin/wilma-mcp",
      "cwd": "/path/to/wilma-mcp"
    }
  }
}
```

Restart Claude Desktop after updating the configuration.

## Running in SSE mode

By default the server runs in `stdio` mode. To run with SSE transport:

```bash
./bin/wilma-mcp --transport sse --listen :8080 --base-url http://localhost:8080 --base-path /mcp
```

## Available Tools

### `get_schedule`
Get the school schedule for a specific date.

**Parameters:**
- `date_str` (optional): Date to get schedule for. Defaults to "today".
  - Supports: "today", "tomorrow", "yesterday"
  - Weekday names: "monday", "tuesday", etc. (English or Finnish)
  - Date formats: "2024-03-15", "15.3.2024"

**Example:** "What's my schedule for Monday?"

### `get_week_schedule`
Get the schedule for a full week.

**Parameters:**
- `start_date` (optional): Start date of the week. Defaults to today.

**Example:** "Show me next week's schedule"

### `get_messages`
Get list of messages from inbox. Each message shows a read/unread indicator (📖 read, 📬 unread).

**Parameters:**
- `folder` (optional): Folder name - "inbox", "sent", or "archive". Defaults to "inbox".
- `limit` (optional): Maximum messages to return. Defaults to 20.

**Example:** "Check my messages"

### `get_message`
Read a specific message with full content. Note: viewing a message automatically marks it as read on the Wilma server.

**Parameters:**
- `message_id`: The ID of the message to read.

**Example:** "Read message 12345"

### `set_message_read`
Explicitly mark a message as read. Useful for marking messages as read without reading their full content. Wilma does not support marking messages as unread — this is a platform limitation.

**Parameters:**
- `message_id`: The ID of the message to mark as read.

**Example:** "Mark message 12345 as read"

### `get_recipients`
Get list of available message recipients (teachers, staff).

**Example:** "Who can I send messages to?"

### `send_message`
Send a new message to a teacher or staff member.

**Parameters:**
- `recipient_id`: ID of the recipient (use `get_recipients` to find IDs)
- `subject`: Message subject
- `body`: Message body/content
- `reply_to_id` (optional): Message ID if this is a reply

**Example:** "Send a message to teacher 123 about homework"

### `reply_to_message`
Reply to an existing message. This is the preferred way to reply since it handles recipient resolution automatically via Wilma's reply form, without needing to look up recipient IDs.

**Parameters:**
- `message_id`: ID of the message to reply to (from `get_messages`)
- `body`: Reply message body/content

**Example:** "Reply to message 12345 saying I'll attend"

## Example Conversations

Once configured, you can ask Claude:

- "What's my schedule today?"
- "Do I have any classes on Friday?"
- "Show me my unread messages"
- "Read the message from my teacher"
- "What time does school start tomorrow?"

## Technical Notes

- Wilma has no official public API. This server reverse-engineers the web interface.
- Authentication uses session cookies obtained via the login flow.
- Schedule data is extracted from embedded JavaScript in the schedule page.
- Message lists use a JSON endpoint; individual messages require HTML parsing.
- **Read/unread tracking**: Wilma's JSON API includes a `Status` field per message — truthy means unread, falsy/absent means read. Viewing a message (GET request) marks it as read server-side. There is no API to mark a message as unread.
- The server may need updates if Wilma's web interface changes.

## Development

```bash
# Format and tidy
gofmt -w ./cmd ./internal
go mod tidy

# Run checks
go test ./...
```

## Future Features (Planned)

- Grades and assessments
- Absence/attendance records
- Upcoming exams
- School news/announcements
- Course listings

## License

MIT License - see [LICENSE](LICENSE) file.

## Disclaimer

This is an unofficial project and is not affiliated with or endorsed by Visma. Use at your own risk. Be respectful of Wilma's terms of service and rate limits.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
