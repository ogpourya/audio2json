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

	"github.com/ogpourya/audio2json/transcribe"
)

const chunkDuration = 15.0 // seconds per chunk

func main() {
	checkFFmpegAndProbe()

	lang := flag.String("lang", "en-US", "Language code (e.g. en-US, fr, es)")
	debug := flag.Bool("debug", false, "Debug mode - show progress and errors")
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	files := getFilesFromArgsOrStdin()
	if len(files) == 0 {
		printHelp()
		return
	}
	files = uniqueFiles(files)

	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", f)
			os.Exit(1)
		}
	}

	type fileResult struct {
		filename string
		text     *string
		err      error
	}

	fileResultsChan := make(chan fileResult, len(files))

	var wg sync.WaitGroup
	maxFileWorkers := 3
	fileJobs := make(chan string, len(files))

	worker := func() {
		defer wg.Done()
		for file := range fileJobs {
			if *debug {
				fmt.Printf("Processing file: %s\n", file)
			}

			duration, err := getAudioDuration(file)
			if err != nil {
				log.Printf("Failed to get duration for %s: %v\n", file, err)
				fileResultsChan <- fileResult{filename: filepath.Base(file), text: nil, err: err}
				continue
			}

			if duration <= chunkDuration {
				if *debug {
					fmt.Printf("Transcribing full file (<=15s): %s\n", file)
				}
				text, err := transcribeFile(file, *lang, *debug)
				fileResultsChan <- fileResult{filename: filepath.Base(file), text: text, err: err}
			} else {
				if *debug {
					fmt.Printf("File >15s, transcribing in chunks: %s\n", file)
				}
				text, err := transcribeFileInChunks(file, duration, *lang, *debug)
				fileResultsChan <- fileResult{filename: filepath.Base(file), text: text, err: err}
			}
		}
	}

	wg.Add(maxFileWorkers)
	for i := 0; i < maxFileWorkers; i++ {
		go worker()
	}

	for _, file := range files {
		fileJobs <- file
	}
	close(fileJobs)

	wg.Wait()
	close(fileResultsChan)

	results := make(map[string]*string)
	for res := range fileResultsChan {
		if res.err != nil {
			log.Printf("Error transcribing %s: %v\n", res.filename, res.err)
		}
		results[res.filename] = res.text
	}

	printJSON(results)
}

func checkFFmpegAndProbe() {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ffmpeg not found. Install and add to PATH.")
		os.Exit(1)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: ffprobe not found. Install and add to PATH.")
		os.Exit(1)
	}
}

func getFilesFromArgsOrStdin() []string {
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
			return nil
		}
		files = flag.Args()
	}
	return files
}

func getAudioDuration(file string) (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var duration float64
	_, err = fmt.Sscanf(string(output), "%f", &duration)
	return duration, err
}

func transcribeFile(file, lang string, debug bool) (*string, error) {
	if debug {
		fmt.Printf("Transcribing file: %s\n", file)
	}
	text, err := transcribe.Transcribe(file, lang)
	if err != nil {
		return nil, err
	}
	if text == nil {
		if debug {
			fmt.Printf("Warning: transcription returned nil text for %s\n", file)
		}
		return nil, fmt.Errorf("transcription returned nil text for %s", file)
	}
	return text, nil
}

func transcribeFileInChunks(file string, duration float64, lang string, debug bool) (*string, error) {
	chunksCount := int(duration / chunkDuration)
	if duration > float64(chunksCount)*chunkDuration {
		chunksCount++
	}

	chunkTexts := make([]*string, chunksCount)
	chunkErrors := make([]error, chunksCount)

	var chunkWg sync.WaitGroup
	chunkJobs := make(chan int, chunksCount)

	chunkWorker := func() {
		defer chunkWg.Done()
		for i := range chunkJobs {
			start := float64(i) * chunkDuration
			length := chunkDuration
			if start+length > duration {
				length = duration - start
			}

			chunkFile := fmt.Sprintf("%s_chunk_%d.wav", file, i)
			if debug {
				fmt.Printf("Extracting chunk %d: %s (start=%.2fs length=%.2fs)\n", i, chunkFile, start, length)
			}

			err := extractChunk(file, start, length, chunkFile)
			if err != nil {
				chunkErrors[i] = fmt.Errorf("chunk extraction error: %v", err)
				continue
			}

			text, err := transcribe.Transcribe(chunkFile, lang)
			os.Remove(chunkFile)
			if err != nil {
				chunkErrors[i] = fmt.Errorf("chunk transcription error: %v", err)
				continue
			}
			if text == nil {
				chunkErrors[i] = fmt.Errorf("chunk transcription returned nil text")
				continue
			}
			chunkTexts[i] = text
		}
	}

	workerCount := 3
	chunkWg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go chunkWorker()
	}

	for i := 0; i < chunksCount; i++ {
		chunkJobs <- i
	}
	close(chunkJobs)

	chunkWg.Wait()

	// Allow partial success — log errors but keep successful chunk texts
	anySuccess := false
	for i, err := range chunkErrors {
		if err != nil {
			// Log warnings only if debug mode is enabled
			if debug {
				log.Printf("Warning: chunk %d error: %v\n", i, err)
			}
		} else {
			anySuccess = true
		}
	}

	if !anySuccess {
		return nil, fmt.Errorf("all chunks failed to transcribe")
	}

	var combined strings.Builder
	for _, t := range chunkTexts {
		if t != nil {
			combined.WriteString(*t + " ")
		}
	}
	finalText := strings.TrimSpace(combined.String())
	return &finalText, nil
}

func extractChunk(input string, start float64, duration float64, chunkFile string) error {
	args := []string{
		"-ss", fmt.Sprintf("%.2f", start),
		"-t", fmt.Sprintf("%.2f", duration),
		"-i", input,
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		chunkFile,
		"-y",
	}
	cmd := exec.Command("ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
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
