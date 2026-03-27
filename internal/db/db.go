package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var LocalDB *sql.DB

func InitDB(dsn string) {
	var err error
	LocalDB, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("Failed to open MySQL: %v — will retry", err)
		go retryDB(dsn)
		return
	}

	// Test connection with retry
	if err = LocalDB.Ping(); err != nil {
		log.Printf("Failed to connect to MySQL: %v — will retry in background", err)
		go retryDB(dsn)
		return
	}

	createTables()
	log.Println("MySQL database initialized successfully.")
}

func retryDB(dsn string) {
	for {
		time.Sleep(5 * time.Second)
		var err error
		LocalDB, err = sql.Open("mysql", dsn)
		if err != nil {
			log.Printf("MySQL retry: open failed: %v", err)
			continue
		}
		if err = LocalDB.Ping(); err != nil {
			log.Printf("MySQL retry: ping failed: %v", err)
			continue
		}
		createTables()
		log.Println("MySQL database initialized successfully (on retry).")
		return
	}
}

func createTables() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS usage_logs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			date VARCHAR(10) UNIQUE,
			messages_sent INT DEFAULT 0,
			messages_failed INT DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS scheduled_messages (
			id INT AUTO_INCREMENT PRIMARY KEY,
			recipient VARCHAR(30) NOT NULL,
			message TEXT NOT NULL,
			scheduled_for DATETIME NOT NULL,
			status VARCHAR(20) DEFAULT 'pending'
		);`,
	}

	for _, query := range queries {
		_, err := LocalDB.Exec(query)
		if err != nil {
			fmt.Printf("Error creating table: %v\n", err)
		}
	}
}

func LogMessageUsage(success bool) {
	if LocalDB == nil {
		return
	}

	today := time.Now().Format("2006-01-02")

	// Ensure row exists for today
	_, _ = LocalDB.Exec(`INSERT IGNORE INTO usage_logs (date) VALUES (?)`, today)

	if success {
		_, _ = LocalDB.Exec(`UPDATE usage_logs SET messages_sent = messages_sent + 1 WHERE date = ?`, today)
	} else {
		_, _ = LocalDB.Exec(`UPDATE usage_logs SET messages_failed = messages_failed + 1 WHERE date = ?`, today)
	}
}

type Metrics struct {
	TotalSent      int `json:"total_sent"`
	TotalFailed    int `json:"total_failed"`
	ScheduledCount int `json:"scheduled_count"`
}

func GetMetrics() (Metrics, error) {
	var m Metrics
	err := LocalDB.QueryRow(`SELECT IFNULL(SUM(messages_sent),0), IFNULL(SUM(messages_failed),0) FROM usage_logs`).Scan(&m.TotalSent, &m.TotalFailed)
	if err != nil {
		m.TotalSent = 0
		m.TotalFailed = 0
	}

	LocalDB.QueryRow(`SELECT COUNT(*) FROM scheduled_messages WHERE status = 'pending'`).Scan(&m.ScheduledCount)
	return m, nil
}

func AddScheduledMessage(recipient, message, scheduledFor string) error {
	_, err := LocalDB.Exec(`INSERT INTO scheduled_messages (recipient, message, scheduled_for) VALUES (?, ?, ?)`,
		recipient, message, scheduledFor)
	return err
}

type ScheduledMessage struct {
	ID        int
	Recipient string
	Message   string
}

func GetPendingMessages(now string) ([]ScheduledMessage, error) {
	rows, err := LocalDB.Query(`SELECT id, recipient, message FROM scheduled_messages WHERE status = 'pending' AND scheduled_for <= ?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ScheduledMessage
	for rows.Next() {
		var m ScheduledMessage
		if err := rows.Scan(&m.ID, &m.Recipient, &m.Message); err == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func UpdateScheduledMessageStatus(id int, status string) error {
	_, err := LocalDB.Exec(`UPDATE scheduled_messages SET status = ? WHERE id = ?`, status, id)
	return err
}
