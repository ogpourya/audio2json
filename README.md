# audio2json

**audio2json** is a simple command-line tool that transcribes audio files into text and outputs the results as JSON. It supports multiple audio formats by converting them to WAV internally using `ffmpeg`, and sends audio data to Google's Speech-to-Text API for transcription.

> **Important:**  
> No API key is required to use this tool. It uses a free Google Speech-to-Text API endpoint provided by [pyTranscriber](https://github.com/raryelcostasouza/pyTranscriber/).  
> Accuracy depends entirely on the Google Speech-to-Text API 🤷‍♂️.

> **Known Limitation:**  
> The audio file length should not exceed 30 seconds (this is the maximum limit, ideally shorter). However, there is no rate limit restriction in the API, so you can write a script to transcribe much longer files. This feature might be added natively in a future update.

## Features

- Transcribes audio files to text (JSON output)  
- Supports multiple files and batch processing via stdin or arguments  
- Configurable language code (default `en-US`)  
- Silent mode by default, with optional debug mode for detailed logs  
- Uses `ffmpeg` to handle audio conversion to required format  
- Concurrent transcription with safe limits to avoid API rate issues  

## Installation

Make sure you have `ffmpeg` installed and accessible in your system `PATH`.

To install `ffmpeg` on Debian/Ubuntu:
```bash
sudo apt update
sudo apt install ffmpeg
```
Then install the tool using Go:
```bash
go install github.com/pzaeemfar/audio2json@latest
```

## Usage

Basic usage:

```bash
audio2json [options] file1 file2 ...
```

Or provide file paths via stdin:

```bash
cat filelist.txt | audio2json [options]
```

### Options:

* `-lang string`
  Language code for transcription (default: `en-US`)

* `-debug`
  Show progress and error messages

* `-help`
  Show help message

### Example:

```bash
audio2json -lang en-US -debug audio1.mp3 audio2.ogg
```

### Supported languages

For a full and up-to-date list of supported language codes, please see the official Google Speech-to-Text API documentation:  
https://cloud.google.com/speech-to-text/docs/languages
