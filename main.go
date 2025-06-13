package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pzaeemfar/audio2json/transcribe"
)

func main() {
	checkFFmpeg()

	lang := flag.String("lang", "en-US", "Language code (e.g. en-US, fr, es)")
	debug := flag.Bool("debug", false, "Debug mode - show progress and errors")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	stdinInfo, _ := os.Stdin.Stat()
	var files []string

	if (stdinInfo.Mode() & os.ModeCharDevice) == 0 {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				files = append(files, line)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("Error reading stdin: %v", err)
		}
	} else {
		if flag.NArg() == 0 {
			printHelp()
			return
		}
		files = flag.Args()
	}

	files = uniqueFiles(files)

	// Check if all files exist, exit if any missing
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", file)
			os.Exit(1)
		}
	}

	type result struct {
		filename string
		text     *string
		err      error
	}

	maxWorkers := 3
	jobs := make(chan string, len(files))
	resultsChan := make(chan result, len(files))

	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for file := range jobs {
			if *debug {
				fmt.Printf("Transcribing %s...\n", file)
			}
			text, err := transcribe.Transcribe(file, *lang)
			if err != nil && *debug {
				log.Printf("Error transcribing %s: %v\n", file, err)
			}
			resultsChan <- result{filename: filepath.Base(file), text: text, err: err}
		}
	}

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go worker()
	}

	for _, file := range files {
		jobs <- file
	}
	close(jobs)

	wg.Wait()
	close(resultsChan)

	results := make(map[string]*string)
	for res := range resultsChan {
		results[res.filename] = res.text
	}

	printJSON(results)
}

func checkFFmpeg() {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ffmpeg not found in PATH. Please install ffmpeg and ensure it is accessible.")
		os.Exit(1)
	}
}

func uniqueFiles(files []string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	return unique
}

func printJSON(data map[string]*string) {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal results: %v", err)
	}
	fmt.Println(string(out))
}

func printHelp() {
	fmt.Println(`audio2json - Transcribe audio files to text (JSON output)

Usage:
  audio2json [options] file1 file2 ...

Options:
  -lang string
        Language code for transcription (default "en-US")
  -debug
        Debug mode - show progress and errors
  -help
        Show this help message

Example:
  audio2json -lang en-US -debug audio1.mp3 audio2.ogg`)
}
