package correlation

import "strings"

// NormalizedCommand removes volatile ports and redacts secret values while retaining unknown arguments.
func NormalizedCommand(executable string, args []string) string {
	values := []string{executable}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--port" || argument == "-p" {
			index++
			continue
		}
		if strings.HasPrefix(argument, "--port=") {
			continue
		}
		if strings.Contains(strings.ToLower(argument), "token") || strings.Contains(strings.ToLower(argument), "secret") || strings.Contains(strings.ToLower(argument), "password") {
			values = append(values, "<redacted>")
			continue
		}
		values = append(values, argument)
	}
	return strings.Join(values, "\x00")
}
