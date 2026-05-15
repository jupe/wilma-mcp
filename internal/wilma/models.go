package wilma

import "time"

type Lesson struct {
	StartTime string
	EndTime   string
	Subject   string
	Teacher   string
	Room      string
	Notes     string
}

type DaySchedule struct {
	Date    time.Time
	Lessons []Lesson
}

type Recipient struct {
	ID     string
	Name   string
	Role   string
	School string
}

type MessageSummary struct {
	ID        string
	Subject   string
	Sender    string
	Timestamp time.Time
	IsRead    bool
	Folder    string
}

type Message struct {
	ID          string
	Subject     string
	Sender      string
	Timestamp   time.Time
	Content     string
	Recipients  []string
	Attachments []string
	IsRead      bool
	Folder      string
}
