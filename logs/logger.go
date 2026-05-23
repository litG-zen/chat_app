package logs

import (
	"fmt"
	"log"
	"os"
	"time"
)

const LOG_DIR = "logs"
const ERR_LOG_FILE = "error.log"
const ACCESS_LOG_FILE = "access.log"
const DATE_FORMAT_LAYOUT = "2006-01-02" // this date is used in Golang for formatting because its Golang's birthday

func GetCurrentDate() string {
	// Fetches the current date and returns it into YYYY_MM_DD format for maintaining current day's log directory
	now := time.Now()
	return fmt.Sprintf("%v", now.Format(DATE_FORMAT_LAYOUT))
}

func GetCurrentLogDir() string {
	cwd, _ := os.Getwd()
	log_dir := fmt.Sprintf("%v/%v/%v", cwd, LOG_DIR, GetCurrentDate())
	return log_dir
}

func CloseLogFiles() {
	log_dir := GetCurrentLogDir()
	log_files := []string{ERR_LOG_FILE, ACCESS_LOG_FILE}
	for _, log_file := range log_files {
		_, err := os.Stat(fmt.Sprintf("%v/%v", log_dir, log_file))
		if err != nil {
			continue
		} else {
			// ToDO: Close the file

			// To close a file in Golang, we need to have it file's memory-location-address
			// as is passed in the fundamental building program named file_handling_using_defer.go/closeFile() function.
			// What had kept me bugging was how can I access memory location of an existing file without opening it just from its file_path...
			// I don't want to jump onto chatgpt and get the answer. If the purpose of this journey has been to learn;
			// lets overcome all the blockers as we learn.

			// Few intutions
			/*
				- using channel approach
				- maintaing `map[log_dir_str] register-block-address` of the access/error log of that day(Since we are maintaing date_string level docs in logs/<YYYY_MM_DD>)
				-
			*/

			// Note: Mimicking how /var/logs/nginx/access.log|error.logs gets auto deleted based on a config defined in /etc/ngit ginx
		}
	}
}

func Logger(log_string string, isErr bool) {
	// Takes the log string and writes it into error.log/access.log based on isErr.
	log_dir := GetCurrentLogDir()

	log_file := ACCESS_LOG_FILE
	if isErr {
		log_file = ERR_LOG_FILE
	}

	// MkdirAll is a no-op if the directory exists and creates parents otherwise,
	// so we don't need a separate Stat probe.
	if err := os.MkdirAll(log_dir, 0o755); err != nil {
		log.Printf("logger: cannot create log dir %s: %v", log_dir, err)
		return
	}

	log_file_path := fmt.Sprintf("%v/%v", log_dir, log_file)

	// O_APPEND alone is read-only (RDONLY=0) and Fprintln silently fails on it —
	// that's why earlier runs produced empty log files. We need WRONLY|APPEND|CREATE.
	f, err := os.OpenFile(log_file_path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		log.Printf("logger: cannot open %s: %v", log_file_path, err)
		return
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, log_string); err != nil {
		log.Printf("logger: write to %s failed: %v", log_file_path, err)
	}
}
