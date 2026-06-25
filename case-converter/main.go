package main

import (
	"common-module/utils"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CaseConverter contains all text transformation methods
type CaseConverter struct{}

// Global instances to avoid repeated allocations
var (
	globalCaseConverter = &CaseConverter{}
	globalColorOutput   = &ColorOutput{}
	titleCaser          = cases.Title(language.English)
)

// RemoveNonAlpha removes non-alphabetic characters from a string, keeping whitespace and alphanumeric
func (cc *CaseConverter) RemoveNonAlpha(s string) string {
	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity
	for _, char := range s {
		if unicode.IsLetter(char) || unicode.IsSpace(char) || unicode.IsNumber(char) {
			result.WriteRune(char)
		}
	}
	return result.String()
}

// ToSnakeCase converts string to snake_case
func (cc *CaseConverter) ToSnakeCase(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "_"))
}

// ToPascalCase converts string to PascalCase
func (cc *CaseConverter) ToPascalCase(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity

	for _, word := range words {
		if len(word) > 0 {
			if result.Len() > 0 {
				result.WriteString(strings.ToUpper(word[:1]))
				result.WriteString(strings.ToLower(word[1:]))
			} else {
				result.WriteString(strings.ToUpper(word[:1]))
				result.WriteString(strings.ToLower(word[1:]))
			}
		}
	}
	return result.String()
}

// ToKebabCase converts string to kebab-case
func (cc *CaseConverter) ToKebabCase(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

// ToConstantCase converts string to CONSTANT_CASE
func (cc *CaseConverter) ToConstantCase(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, " ", "_"))
}

// ToPathCase converts string to path/case
func (cc *CaseConverter) ToPathCase(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "/"))
}

// ToCamelCase converts string to camelCase
func (cc *CaseConverter) ToCamelCase(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity

	// First word in lowercase
	if len(words[0]) > 0 {
		result.WriteString(strings.ToLower(words[0]))
	}

	// Subsequent words with first letter uppercase
	for i := 1; i < len(words); i++ {
		if len(words[i]) > 0 {
			result.WriteString(strings.ToUpper(words[i][:1]))
			result.WriteString(strings.ToLower(words[i][1:]))
		}
	}
	return result.String()
}

// ToTitleCase converts string to Title Case
func (cc *CaseConverter) ToTitleCase(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity

	for i, word := range words {
		if i > 0 {
			result.WriteByte(' ')
		}
		if len(word) > 0 {
			result.WriteString(strings.ToUpper(word[:1]))
			result.WriteString(strings.ToLower(word[1:]))
		}
	}
	return result.String()
}

// ToDotCase converts string to dot.case
func (cc *CaseConverter) ToDotCase(s string) string {
	return strings.Join(strings.Fields(s), ".")
}

// FromSnakeCase converts snake_case to normal text
func (cc *CaseConverter) FromSnakeCase(s string) string {
	words := strings.Split(s, "_")
	if len(words) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity

	for i, word := range words {
		if i > 0 {
			result.WriteByte(' ')
		}
		if len(word) > 0 {
			result.WriteString(strings.ToUpper(word[:1]))
			result.WriteString(strings.ToLower(word[1:]))
		}
	}
	return result.String()
}

// FromPascalCase converts PascalCase to normal text
func (cc *CaseConverter) FromPascalCase(s string) string {
	if len(s) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s) + 10) // Pre-allocate capacity with some extra space

	for i, char := range s {
		if i > 0 && unicode.IsUpper(char) {
			result.WriteByte(' ')
		}
		result.WriteRune(char)
	}
	return result.String()
}

// FromCamelCase converts camelCase to normal text
func (cc *CaseConverter) FromCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s) + 10) // Pre-allocate capacity with some extra space

	for i, char := range s {
		if i > 0 && unicode.IsUpper(char) {
			result.WriteByte(' ')
		}
		result.WriteRune(char)
	}
	return result.String()
}

// FromKebabCase converts kebab-case to normal text
func (cc *CaseConverter) FromKebabCase(s string) string {
	words := strings.Split(s, "-")
	if len(words) == 0 {
		return s
	}

	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity

	for i, word := range words {
		if i > 0 {
			result.WriteByte(' ')
		}
		if len(word) > 0 {
			result.WriteString(strings.ToUpper(word[:1]))
			result.WriteString(strings.ToLower(word[1:]))
		}
	}
	return result.String()
}

// ColorOutput provides colored terminal output
type ColorOutput struct{}

// Green returns green colored text
func (co *ColorOutput) Green(msg string) string {
	return fmt.Sprintf("\033[42m\033[1;30m %s \033[0m", msg)
}

// Blue returns blue colored text
func (co *ColorOutput) Blue(msg string) string {
	return fmt.Sprintf("\033[44m\033[1;30m %s \033[0m", msg)
}

// detectCaseType detects the input case type to avoid unnecessary conversions
func detectCaseType(text string) string {
	if strings.Contains(text, " ") {
		return "normal"
	}
	if strings.Contains(text, "_") {
		return "snake"
	}
	if strings.Contains(text, "-") {
		return "kebab"
	}
	if strings.Contains(text, ".") {
		return "dot"
	}
	if strings.Contains(text, "/") {
		return "path"
	}
	// Check for camelCase or PascalCase
	for i, char := range text {
		if i > 0 && unicode.IsUpper(char) {
			return "camel_or_pascal"
		}
	}
	return "unknown"
}

// normalizeText optimizes text normalization based on detected case type
func normalizeText(text string) string {
	caseType := detectCaseType(text)

	switch caseType {
	case "normal":
		return text
	case "snake":
		return globalCaseConverter.FromSnakeCase(text)
	case "kebab":
		return globalCaseConverter.FromKebabCase(text)
	case "dot":
		return strings.ReplaceAll(text, ".", " ")
	case "path":
		return strings.ReplaceAll(text, "/", " ")
	case "camel_or_pascal":
		// Try camel case first, then pascal
		result := globalCaseConverter.FromCamelCase(text)
		if result != text {
			return result
		}
		return globalCaseConverter.FromPascalCase(text)
	default:
		// Try all conversions as fallback
		result := globalCaseConverter.FromCamelCase(text)
		if result != text {
			return result
		}
		result = globalCaseConverter.FromSnakeCase(text)
		if result != text {
			return result
		}
		result = globalCaseConverter.FromKebabCase(text)
		if result != text {
			return result
		}
		return globalCaseConverter.FromPascalCase(text)
	}
}

// ProcessCaseConversions processes text and returns all case conversions
func ProcessCaseConversions(text string) map[string]string {
	// Normalize text efficiently
	normalized := normalizeText(text)

	// Clean up the text
	words := strings.Fields(strings.TrimSpace(normalized))
	cleanText := globalCaseConverter.RemoveNonAlpha(strings.Join(words, " "))
	cleanText = strings.ToLower(cleanText)

	if len(cleanText) == 0 {
		cleanText = strings.ToLower(strings.TrimSpace(text))
	}

	// Pre-allocate the result map
	result := make(map[string]string, 13)

	// Use cached instances and avoid repeated allocations
	result["normal"] = cleanText
	result["upper"] = strings.ToUpper(cleanText)
	result["lower"] = strings.ToLower(cleanText)

	if len(cleanText) > 0 {
		result["capitalized"] = strings.ToUpper(cleanText[:1]) + strings.ToLower(cleanText[1:])
	} else {
		result["capitalized"] = cleanText
	}

	result["swapped"] = swapCase(cleanText)
	result["snake_case"] = globalCaseConverter.ToSnakeCase(cleanText)
	result["kebab_case"] = globalCaseConverter.ToKebabCase(cleanText)
	result["camel_case"] = globalCaseConverter.ToCamelCase(cleanText)
	result["pascal_case"] = globalCaseConverter.ToPascalCase(cleanText)
	result["constant_case"] = globalCaseConverter.ToConstantCase(cleanText)
	result["title_case"] = globalCaseConverter.ToTitleCase(cleanText)
	result["dot_case"] = globalCaseConverter.ToDotCase(cleanText)
	result["path_case"] = globalCaseConverter.ToPathCase(cleanText)
	result["pascal_kebab"] = strings.ReplaceAll(globalCaseConverter.ToTitleCase(cleanText), " ", "-")

	return result
}

// swapCase swaps the case of each character
func swapCase(s string) string {
	var result strings.Builder
	result.Grow(len(s)) // Pre-allocate capacity
	for _, char := range s {
		if unicode.IsUpper(char) {
			result.WriteRune(unicode.ToLower(char))
		} else if unicode.IsLower(char) {
			result.WriteRune(unicode.ToUpper(char))
		} else {
			result.WriteRune(char)
		}
	}
	return result.String()
}

// Pre-defined sorted keys to avoid sorting every time
var sortedKeys = []string{
	"normal", "upper", "lower", "capitalized", "swapped",
	"snake_case", "kebab_case", "camel_case", "pascal_case",
	"constant_case", "title_case", "dot_case", "path_case", "pascal_kebab",
}

// PrintConversions prints all case conversions for a given line
func PrintConversions(line string) {
	fmt.Printf("\n%s: %s\n", globalColorOutput.Blue("Original"), line)
	conversions := ProcessCaseConversions(line)

	for _, formatName := range sortedKeys {
		if converted, exists := conversions[formatName]; exists {
			displayName := strings.ReplaceAll(formatName, "_", " ")
			displayName = titleCaser.String(displayName)
			fmt.Printf("%s: %s\n", globalColorOutput.Green(displayName), converted)
		}
	}
}

var (
	file   string
	all    bool
	format string
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "case-converter",
		Short: "Case Converter CLI Tool - A text case conversion utility",
		Long: `Case Converter CLI Tool - A command-line tool for text case conversion and transformation.

Examples:
  # Convert text to various cases
  case-converter "hello world"

  # Convert from file
  case-converter -f input.txt

  # Show all case conversions
  case-converter "hello world" --all

  # Output specific format only
  case-converter "hello world" --format snake`,
		Run: func(cmd *cobra.Command, args []string) {
			// Clear screen
			utils.CLS()

			var inputText string
			if file != "" {
				content, err := os.ReadFile(file)
				if err != nil {
					fmt.Printf("Error reading file: %v\n", err)
					os.Exit(1)
				}
				inputText = string(content)
			} else if len(args) > 0 {
				inputText = args[0]
			} else {
				cmd.Help()
				return
			}

			// Split by lines if multiple lines
			lines := strings.Split(strings.TrimSpace(inputText), "\n")

			if format != "" {
				// Output specific format
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						conversions := ProcessCaseConversions(line)
						if result, exists := conversions[format]; exists {
							fmt.Println(result)
						} else {
							fmt.Println(line)
						}
					}
				}
			} else if all {
				// Output all formats
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						PrintConversions(line)
					}
				}
			} else {
				// Default: show all formats for first line
				if len(lines) > 0 {
					line := strings.TrimSpace(lines[0])
					if line != "" {
						PrintConversions(line)
					}
				}
			}
		},
	}

	rootCmd.Flags().StringVarP(&file, "file", "f", "", "Input file containing text to convert")
	rootCmd.Flags().BoolVar(&all, "all", false, "Show all case conversions")
	rootCmd.Flags().StringVar(&format, "format", "", "Specific format to output (normal, upper, lower, snake, kebab, camel, pascal, constant, title, dot, path)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
