package middleware

import (
	"testing"
)

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal string",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "string with null bytes",
			input:    "Hello\x00World",
			expected: "HelloWorld",
		},
		{
			name:     "string with control characters",
			input:    "Hello\x01\x02World",
			expected: "HelloWorld",
		},
		{
			name:     "string with allowed whitespace",
			input:    "Hello\nWorld\t!",
			expected: "Hello\nWorld\t!",
		},
		{
			name:     "string with carriage return",
			input:    "Hello\rWorld",
			expected: "Hello\rWorld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		isValid bool
	}{
		{
			name:    "valid email",
			email:   "test@example.com",
			isValid: true,
		},
		{
			name:    "valid email with subdomain",
			email:   "user@mail.example.com",
			isValid: true,
		},
		{
			name:    "invalid email - no @",
			email:   "testexample.com",
			isValid: false,
		},
		{
			name:    "invalid email - no domain",
			email:   "test@",
			isValid: false,
		},
		{
			name:    "invalid email - no username",
			email:   "@example.com",
			isValid: false,
		},
		{
			name:    "empty email",
			email:   "",
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateEmail(tt.email)
			if result != tt.isValid {
				t.Errorf("ValidateEmail(%q) = %v, want %v", tt.email, result, tt.isValid)
			}
		})
	}
}

// TestValidateStruct_Phone tests phone validation through ValidateStruct
func TestValidateStruct_Phone(t *testing.T) {
	type TestStruct struct {
		Phone string `validate:"phone"`
	}

	tests := []struct {
		name    string
		phone   string
		isValid bool
	}{
		{
			name:    "valid 10 digit phone",
			phone:   "1234567890",
			isValid: true,
		},
		{
			name:    "valid phone with formatting",
			phone:   "+1 (234) 567-8900",
			isValid: true,
		},
		{
			name:    "valid international phone",
			phone:   "+33123456789",
			isValid: true,
		},
		{
			name:    "invalid - too short",
			phone:   "1234",
			isValid: false,
		},
		{
			name:    "invalid - too long (21+ digits)",
			phone:   "123456789012345678901", // 21 digits
			isValid: false,
		},
		{
			name:    "valid - letters stripped, enough digits remain",
			phone:   "123abc7890", // Letters stripped = 10 digits
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := TestStruct{Phone: tt.phone}
			errors := ValidateStruct(obj)
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with phone %q: hasErrors=%v, want isValid=%v", tt.phone, hasErrors, tt.isValid)
			}
		})
	}
}

// TestValidateStruct_SafeURL tests the safeurl validator through ValidateStruct
func TestValidateStruct_SafeURL(t *testing.T) {
	type TestStruct struct {
		URL string `validate:"safeurl"`
	}

	tests := []struct {
		name    string
		url     string
		isValid bool
	}{
		{name: "empty allowed", url: "", isValid: true},
		{name: "https", url: "https://example.com", isValid: true},
		{name: "http", url: "http://example.com/path", isValid: true},
		{name: "scheme-less host", url: "example.com", isValid: true},
		{name: "host with port", url: "example.com:8080/x", isValid: true},
		{name: "mailto allowed", url: "mailto:a@b.com", isValid: true},
		{name: "javascript blocked", url: "javascript:alert(1)", isValid: false},
		{name: "uppercase javascript blocked", url: "JavaScript:alert(1)", isValid: false},
		{name: "whitespace-obfuscated javascript blocked", url: "java\tscript:alert(1)", isValid: false},
		{name: "leading space javascript blocked", url: "  javascript:alert(1)", isValid: false},
		{name: "data blocked", url: "data:text/html,<script>", isValid: false},
		{name: "vbscript blocked", url: "vbscript:msgbox(1)", isValid: false},
		{name: "file blocked", url: "file:///etc/passwd", isValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateStruct(TestStruct{URL: tt.url})
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with url %q: hasErrors=%v, want isValid=%v", tt.url, hasErrors, tt.isValid)
			}
		})
	}
}

// TestValidateStruct_HTTPURL tests the httpurl validator (T41) through
// ValidateStruct. The mirror frontend test (frontend/src/utils/
// linkResolution.test.ts) must match this table exactly — the two
// implementations disagreeing is the actual risk this ticket exists to close.
func TestValidateStruct_HTTPURL(t *testing.T) {
	type TestStruct struct {
		URL string `validate:"httpurl"`
	}

	tests := []struct {
		name    string
		url     string
		isValid bool
	}{
		{name: "empty allowed", url: "", isValid: true},
		{name: "whitespace allowed", url: "   ", isValid: true},
		{name: "https accepted", url: "https://example.com/path?q=1", isValid: true},
		{name: "http accepted", url: "http://example.com", isValid: true},
		{name: "http with port accepted", url: "http://immich:2283", isValid: true},
		{name: "scheme-less rejected", url: "example.com", isValid: false},
		{name: "host with port rejected", url: "example.com:8080/x", isValid: false},
		{name: "protocol-relative rejected", url: "//example.com/x", isValid: false},
		{name: "mailto rejected", url: "mailto:a@b.com", isValid: false},
		{name: "javascript rejected", url: "javascript:alert(1)", isValid: false},
		{name: "uppercase javascript rejected", url: "JavaScript:alert(1)", isValid: false},
		{name: "whitespace-obfuscated javascript rejected", url: "java\tscript:alert(1)", isValid: false},
		{name: "data rejected", url: "data:text/html,<script>", isValid: false},
		{name: "vbscript rejected", url: "vbscript:msgbox(1)", isValid: false},
		{name: "file rejected", url: "file:///etc/passwd", isValid: false},
		{name: "blob rejected", url: "blob:https://example.com/abc-123", isValid: false},
		{name: "intent rejected", url: "intent://scan/#Intent;scheme=zebra;end", isValid: false},
		{name: "ms-msdt rejected", url: "ms-msdt:/id:PCWAlert;S:RunProgram;X:cmd", isValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateStruct(TestStruct{URL: tt.url})
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with url %q: hasErrors=%v, want isValid=%v", tt.url, hasErrors, tt.isValid)
			}
		})
	}
}

func TestValidateStruct_Birthday(t *testing.T) {
	type TestStruct struct {
		Birthday string `validate:"birthday"`
	}

	tests := []struct {
		name    string
		date    string
		isValid bool
	}{
		{
			name:    "valid date - YYYY-MM-DD",
			date:    "1990-01-15",
			isValid: true,
		},
		{
			name:    "valid date without year - --MM-DD",
			date:    "--01-15",
			isValid: true, // Matches --MM-DD (year optional)
		},
		{
			name:    "valid leap year date",
			date:    "2000-02-29",
			isValid: true,
		},
		{
			name:    "invalid format - US style",
			date:    "01/15/1990",
			isValid: false,
		},
		{
			name:    "invalid format - European style",
			date:    "15.01.1990",
			isValid: false,
		},
		{
			name:    "invalid - not a date",
			date:    "not-a-date",
			isValid: false,
		},
		{
			name:    "empty string is valid (use omitempty or required for mandatory)",
			date:    "",
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := TestStruct{Birthday: tt.date}
			errors := ValidateStruct(obj)
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with birthday %q: hasErrors=%v, want isValid=%v", tt.date, hasErrors, tt.isValid)
			}
		})
	}
}

// TestValidateStruct_NoAtSign tests that usernames cannot contain @ character
func TestValidateStruct_NoAtSign(t *testing.T) {
	type TestStruct struct {
		Username string `validate:"no_at_sign"`
	}

	tests := []struct {
		name    string
		input   string
		isValid bool
	}{
		{
			name:    "valid username without @",
			input:   "johndoe",
			isValid: true,
		},
		{
			name:    "valid username with numbers",
			input:   "john123",
			isValid: true,
		},
		{
			name:    "valid username with underscore",
			input:   "john_doe",
			isValid: true,
		},
		{
			name:    "valid username with 1 character",
			input:   "j",
			isValid: true,
		},
		{
			name:    "valid username with 2 characters (non-latin)",
			input:   "象形",
			isValid: true,
		},
		{
			name:    "invalid - contains @",
			input:   "john@doe",
			isValid: false,
		},
		{
			name:    "invalid - email-like",
			input:   "john@example.com",
			isValid: false,
		},
		{
			name:    "empty string is valid (no @ present)",
			input:   "",
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := TestStruct{Username: tt.input}
			errors := ValidateStruct(obj)
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with username %q: hasErrors=%v, want isValid=%v", tt.input, hasErrors, tt.isValid)
			}
		})
	}
}

// TestValidateStruct_BirthdayIsLexicalOnly pins the DATE-01 decision that the
// birthday validator is deliberately lexical (validateBirthday's doc comment,
// docs/adrs/0015-temporal-semantics.md): it accepts any string matching the
// two canonical shapes — including calendar-impossible month/day values like
// 1990-13-40 or --02-30 — and rejects anything outside those shapes. DATE-02
// (issue #483) owns pathological values: downstream handling (DaysUntilBirthday
// and friends) must never turn an accepted-but-impossible value into a real
// date such as 1 January year zero, which the services test
// TestDaysUntilBirthday_AbsentAndGarbageNeverBecomeJan1YearZero pins.
func TestValidateStruct_BirthdayIsLexicalOnly(t *testing.T) {
	type TestStruct struct {
		Birthday string `validate:"birthday"`
	}

	tests := []struct {
		name    string
		date    string
		isValid bool
	}{
		{
			name:    "out-of-range month passes the lexical check",
			date:    "1990-13-15",
			isValid: true,
		},
		{
			name:    "out-of-range day passes the lexical check",
			date:    "1990-01-40",
			isValid: true,
		},
		{
			name:    "both month and day out of range passes the lexical check",
			date:    "1990-99-99",
			isValid: true,
		},
		{
			name:    "year-less out-of-range month passes the lexical check",
			date:    "--13-40",
			isValid: true,
		},
		{
			name:    "year zero passes the lexical check",
			date:    "0000-01-01",
			isValid: true,
		},
		{
			name:    "far-future year passes the lexical check",
			date:    "9999-12-31",
			isValid: true,
		},
		{
			name:    "year-less 29 February passes",
			date:    "--02-29",
			isValid: true,
		},
		{
			name:    "shape break: DD-MM-YYYY is rejected",
			date:    "15-03-1985",
			isValid: false,
		},
		{
			name:    "shape break: three-digit year is rejected",
			date:    "123-01-15",
			isValid: false,
		},
		{
			name:    "shape break: single-digit month is rejected",
			date:    "1990-1-15",
			isValid: false,
		},
		{
			name:    "surrounding whitespace is rejected by the anchors",
			date:    " 1990-01-15",
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := TestStruct{Birthday: tt.date}
			errors := ValidateStruct(obj)
			hasErrors := len(errors) > 0
			if hasErrors == tt.isValid {
				t.Errorf("ValidateStruct with birthday %q: hasErrors=%v, want isValid=%v", tt.date, hasErrors, tt.isValid)
			}
		})
	}
}
