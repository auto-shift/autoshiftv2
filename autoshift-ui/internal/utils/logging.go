package utils

import (
	"bytes"
	"log"
	"os"
	"time"
)

var (
	LogBuffer bytes.Buffer
	GitLog    = log.New(&LogBuffer, "git: ", log.Ldate|log.Ltime)
	OcpLog    = log.New(&LogBuffer, "ocp: ", log.Ldate|log.Ltime)
)

func LoggingSetup() {
	logPath := "../../data/logs/"
	_, err := os.Stat(logPath)
	if err != nil {
		log.Println(err)
	} else {
		logFile, err := os.OpenFile(logPath+time.DateTime+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		defer logFile.Close() // Ensure the file is closed when the program exits

		// Set the log output to the file
		log.SetOutput(logFile)
	}
}
