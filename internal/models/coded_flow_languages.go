package models

import (
	"fmt"
	"strings"
)

// CodedFlowLanguage is one selectable language for coded-flow detection.
type CodedFlowLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// CodedFlowSupportedLanguages is the catalog of language codes/names used when
// prompting AI and labeling detected languages. Store-level lists may use these codes.
var CodedFlowSupportedLanguages = []CodedFlowLanguage{
	{Code: "en", Name: "English"},
	{Code: "ar", Name: "Arabic"},
	{Code: "manglish", Name: "Manglish"},
	{Code: "as", Name: "Assamese"},
	{Code: "bn", Name: "Bengali"},
	{Code: "brx", Name: "Bodo"},
	{Code: "doi", Name: "Dogri"},
	{Code: "gu", Name: "Gujarati"},
	{Code: "hi", Name: "Hindi"},
	{Code: "kn", Name: "Kannada"},
	{Code: "ks", Name: "Kashmiri"},
	{Code: "kok", Name: "Konkani"},
	{Code: "mai", Name: "Maithili"},
	{Code: "ml", Name: "Malayalam"},
	{Code: "mni", Name: "Manipuri"},
	{Code: "mr", Name: "Marathi"},
	{Code: "ne", Name: "Nepali"},
	{Code: "or", Name: "Odia"},
	{Code: "pa", Name: "Punjabi"},
	{Code: "sa", Name: "Sanskrit"},
	{Code: "sat", Name: "Santali"},
	{Code: "sd", Name: "Sindhi"},
	{Code: "ta", Name: "Tamil"},
	{Code: "te", Name: "Telugu"},
	{Code: "ur", Name: "Urdu"},
}

var codedFlowLanguageCodes = func() map[string]string {
	out := make(map[string]string, len(CodedFlowSupportedLanguages))
	for _, lang := range CodedFlowSupportedLanguages {
		out[lang.Code] = lang.Name
	}
	return out
}()

// CodedFlowLanguageName returns the display name for code, or empty.
func CodedFlowLanguageName(code string) string {
	return codedFlowLanguageCodes[strings.TrimSpace(code)]
}

// ValidCodedFlowLanguage reports whether code is in the admin catalog.
func ValidCodedFlowLanguage(code string) bool {
	_, ok := codedFlowLanguageCodes[strings.TrimSpace(code)]
	return ok
}

// NormalizeCodedFlowLanguages validates and de-duplicates language codes.
// Empty input is valid and means all languages.
func NormalizeCodedFlowLanguages(raw []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, code := range raw {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if !ValidCodedFlowLanguage(code) {
			return nil, fmt.Errorf("unsupported language: %s", code)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out, nil
}
