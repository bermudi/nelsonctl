package pipeline

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Task represents a single checklist item inside a phase.
type Task struct {
	Text string
	Done bool
}

// Phase represents a markdown phase section in tasks.md.
type Phase struct {
	Number int
	Name   string
	Tasks  []Task
}

var (
	phaseHeadingRe = regexp.MustCompile(`^##\s+Phase\s+(\d+):\s*(.+?)\s*$`)
	taskRe         = regexp.MustCompile(`^-\s+\[( |x|X)\]\s+(.+?)\s*$`)
)

// ParseTasksMarkdown extracts phases and checkbox tasks from a markdown reader.
func ParseTasksMarkdown(r io.Reader) ([]Phase, error) {
	scanner := bufio.NewScanner(r)
	var phases []Phase
	var current *Phase
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if matches := phaseHeadingRe.FindStringSubmatch(line); matches != nil {
			num, err := strconv.Atoi(matches[1])
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid phase number %q: %w", lineNo, matches[1], err)
			}

			phases = append(phases, Phase{Number: num, Name: strings.TrimSpace(matches[2])})
			current = &phases[len(phases)-1]
			continue
		}

		if matches := taskRe.FindStringSubmatch(line); matches != nil {
			if current == nil {
				return nil, fmt.Errorf("line %d: task encountered before any phase heading", lineNo)
			}

			current.Tasks = append(current.Tasks, Task{
				Text: strings.TrimSpace(matches[2]),
				Done: strings.EqualFold(matches[1], "x"),
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return phases, nil
}

// ParseTasksFile reads a markdown file from disk and parses its phases.
func ParseTasksFile(path string) ([]Phase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseTasksMarkdown(f)
}
