package protocol

import (
	"errors"
	"strings"
)

// SplitCommand splits a command line into its words the way a shell quotes
// them, and nothing more: 'single' and "double" quotes keep spaces, a
// backslash keeps the next character. No variable, glob, pipe or ; does
// anything: they stay text in the words. For those, run sh -c '…'.
func SplitCommand(line string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	quote := rune(0)
	escaped := false
	for _, char := range line {
		switch {
		case escaped:
			word.WriteRune(char)
			escaped = false
		case char == '\\' && quote != '\'':
			escaped = true
			inWord = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
			inWord = true
		case char == ' ' || char == '\t' || char == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(char)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if escaped {
		return nil, errors.New("the command ends with a backslash")
	}
	if inWord {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return nil, errors.New("no command")
	}
	return words, nil
}

// JoinCommand writes argv as one line SplitCommand reads back, quoting the
// words that need it, for people to read.
func JoinCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for index, word := range argv {
		if word != "" && !strings.ContainsAny(word, " \t\n'\"\\$`|&;<>()*?[]{}#~") {
			quoted[index] = word
			continue
		}
		quoted[index] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
