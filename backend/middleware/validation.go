package middleware

import (
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// Global validator instance
var validate *validator.Validate

func init() {
	validate = validator.New()

	// Register custom validators
	validate.RegisterValidation("phone", validatePhone)
	validate.RegisterValidation("birthday", validateBirthday)
	validate.RegisterValidation("strong_password", validateStrongPassword)
	validate.RegisterValidation("unique_circles", validateUniqueCircles)
	validate.RegisterValidation("no_at_sign", validateNoAtSign)
	validate.RegisterValidation("safeurl", validateSafeURL)
	validate.RegisterValidation("httpurl", validateHTTPURL)
	validate.RegisterValidation("relation_type", validateRelationType)
	validate.RegisterValidation("fielddefprojection", validateFieldDefinitionProjection)
	validate.RegisterValidation("life_event_category", validateLifeEventCategory)
}

// ValidationError represents a validation error response
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateStruct validates a struct and returns formatted errors
func ValidateStruct(obj interface{}) []ValidationError {
	var errors []ValidationError

	err := validate.Struct(obj)
	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			errors = append(errors, ValidationError{
				Field:   err.Field(),
				Message: formatValidationError(err),
			})
		}
	}

	return errors
}

// formatValidationError creates user-friendly error messages
func formatValidationError(err validator.FieldError) string {
	field := err.Field()

	switch err.Tag() {
	case "required":
		return field + " is required"
	case "email":
		return field + " must be a valid email address"
	case "min":
		return field + " must be at least " + err.Param() + " characters"
	case "max":
		return field + " must be at most " + err.Param() + " characters"
	case "phone":
		return field + " must be a valid phone number"
	case "birthday":
		return field + " must be in YYYY-MM-DD format (use --MM-DD if year unknown)"
	case "strong_password":
		return field + " is too weak. Use a longer password (15+ characters) or a passphrase (20+ characters). Avoid common passwords."
	case "unique_circles":
		return field + " cannot contain duplicate circles"
	case "no_at_sign":
		return field + " cannot contain the @ character"
	case "url":
		return field + " must be a valid URL"
	case "safeurl":
		return field + " uses an unsafe URL scheme"
	case "httpurl":
		return field + " must be an http:// or https:// URL"
	case "relation_type":
		return field + " must be a known relationship type"
	default:
		return field + " is invalid"
	}
}

// SanitizeString removes potentially dangerous characters and trims whitespace
func SanitizeString(s string) string {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")

	// Remove control characters except newlines and tabs
	s = removeControlChars(s)

	return s
}

// removeControlChars removes control characters except newlines and tabs
func removeControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

// validatePhone validates phone number format
// Accepts: +1234567890, (123) 456-7890, 123-456-7890, etc.
func validatePhone(fl validator.FieldLevel) bool {
	phone := fl.Field().String()
	if phone == "" {
		return true // Allow empty (use 'required' tag if needed)
	}

	// Remove common formatting characters
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) || r == '+' {
			return r
		}
		return -1
	}, phone)

	// Must have between 5 and 20 digits
	if len(cleaned) < 5 || len(cleaned) > 20 {
		return false
	}

	return true
}

// normalizeSchemeURL strips everything ≤ U+0020 (which is what defeats the
// `java&Tab;script:` bypass — the browser's URL parser drops these control
// characters before it ever looks at the scheme, so a validator that keeps
// them is checking a different string than the browser) and lowercases the
// result, ready for a leading-scheme check. Shared by validateSafeURL and
// validateHTTPURL so the two validators' normalization cannot drift.
func normalizeSchemeURL(raw string) string {
	return strings.Map(func(r rune) rune {
		if r <= ' ' {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(raw))
}

// rejects values whose URL scheme can execute scripts when the value is rendered as link
func validateSafeURL(fl validator.FieldLevel) bool {
	raw := strings.TrimSpace(fl.Field().String())
	if raw == "" {
		return true
	}
	normalized := normalizeSchemeURL(raw)
	// Only a leading "scheme:" is dangerous; a bare "host:port" or path is fine.
	if i := strings.IndexByte(normalized, ':'); i > 0 {
		switch normalized[:i] {
		case "javascript", "data", "vbscript", "file":
			return false
		}
	}
	return true
}

// validateHTTPURL is the `httpurl` validator (T41) for the fields whose value
// means "a web page" — a gift's product link, an agenda item's reference
// article, an external system's deep link, an Immich base URL. It is an
// allowlist, deliberately stricter than validateSafeURL's blocklist: after the
// same normalization, only http/https are accepted and everything else — an
// unknown scheme (`blob:`, `intent:`, `ms-msdt:`, ...), a known-dangerous one
// (`javascript:`, `data:`, ...), or no scheme at all — is rejected. An empty
// value is allowed; fields opt into `required` separately.
func validateHTTPURL(fl validator.FieldLevel) bool {
	raw := strings.TrimSpace(fl.Field().String())
	if raw == "" {
		return true
	}
	normalized := normalizeSchemeURL(raw)
	i := strings.IndexByte(normalized, ':')
	if i <= 0 {
		return false
	}
	scheme := normalized[:i]
	return scheme == "http" || scheme == "https"
}

// validateBirthday validates date format (YYYY-MM-DD or --MM-DD).
//
// Accepted lexical set — exactly two shapes, the date-only / partial-date
// boundary documented in docs/adrs/0015-temporal-semantics.md (mirrored, by
// hand, in models/contact_record.go's birthdayFormatRE and
// services/import_service.go's IsValidBirthdayFormat — models cannot import
// middleware):
//
//	YYYY-MM-DD   a whole calendar date (year, month, day)
//	--MM-DD      a year-less partial date (RFC 6350 year-less convention)
//
// The check is lexical only: calendar-range validity (1990-13-45, 1990-99-99)
// is deliberately not enforced here — the reduced-precision forms those
// strings could otherwise represent (month 99, day 45) are not valid RFC
// partial dates either, and DATE-02 (issue #483) owns pathological-date
// handling. Surrounding whitespace is rejected by the anchors.
func validateBirthday(fl validator.FieldLevel) bool {
	birthday := fl.Field().String()
	if birthday == "" {
		return true // Allow empty (use 'required' tag if needed)
	}

	// Check format YYYY-MM-DD or --MM-DD (ISO 8601 format, year optional)
	match, _ := regexp.MatchString(`^(--|\d{4}-)\d{2}-\d{2}$`, birthday)
	return match
}

// validateStrongPassword checks password strength based on entropy
func validateStrongPassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	// Check if it's a common password
	if IsCommonPassword(password) {
		return false
	}

	// Check entropy
	err := ValidatePasswordStrength(password)
	return err == nil
}

// validateUniqueCircles checks that all circles in the slice are unique (case-insensitive)
func validateUniqueCircles(fl validator.FieldLevel) bool {
	circles := fl.Field()
	if circles.Kind() != reflect.Slice {
		return true // Not a slice, let other validators handle this
	}

	seen := make(map[string]bool)
	for i := 0; i < circles.Len(); i++ {
		circle := circles.Index(i)
		if circle.Kind() != reflect.String {
			continue // Skip non-string elements
		}

		circleValue := strings.ToLower(strings.TrimSpace(circle.String()))
		if circleValue == "" {
			continue // Skip empty strings
		}

		if seen[circleValue] {
			return false // Duplicate found
		}
		seen[circleValue] = true
	}

	return true
}

// validateNoAtSign checks that a string field does not contain the @ character
func validateNoAtSign(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	return !strings.Contains(value, "@")
}

// validateRelationType checks a RelationshipEdge.Type value against
// models.IsKnownRelationType, so the type registry (models/relationship_
// type_registry.go) stays the single source of truth for valid tokens
// rather than a second hardcoded list drifting out of sync with it.
func validateRelationType(fl validator.FieldLevel) bool {
	return models.IsKnownRelationType(fl.Field().String())
}

// validateLifeEventCategory checks a LifeEvent.Category value against
// models.IsKnownLifeEventCategory (T36, models/life_event_type_registry.go),
// following validateRelationType's own registration style so the registry
// stays the single source of truth for valid category tokens rather than a
// second hardcoded `oneof=...` list drifting out of sync with it.
func validateLifeEventCategory(fl validator.FieldLevel) bool {
	return models.IsKnownLifeEventCategory(fl.Field().String())
}

// fieldDefinitionProjectionPattern matches FieldDefinition.Projection (
// docs/adrs/0001-neutral-hub-and-spoke-contact-model.md): "internal-only" (default, never
// exported) or "vcard:X-<NAME>" (projects via the existing JCardProp/
// Passthrough machinery -- see models/contact_record.go's projectCustomFields).
// The doc's third option, a raw "jscontact:<pointer>" projection, is
// deliberately not supported: JSContact's Card.VCardProps already *is*
// Passthrough.VCard copied through verbatim (jscontact/adapter.go), so a
// vcard:-projected field already reaches vCard3, vCard4, and JSContact via
// this one mechanism -- a separate JSContact-only path has no current
// justification.
var fieldDefinitionProjectionPattern = regexp.MustCompile(`^(internal-only|vcard:X-[A-Za-z0-9-]+)$`)

// validateFieldDefinitionProjection checks a FieldDefinition.Projection value
// against fieldDefinitionProjectionPattern, following validateRelationType's
// own registration style for a cross-cutting, registry-backed custom check.
func validateFieldDefinitionProjection(fl validator.FieldLevel) bool {
	return fieldDefinitionProjectionPattern.MatchString(fl.Field().String())
}

// ValidateEmail validates email format with regex
func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// ValidateVar validates a single value against a validator tag string,
// through the same package-level validate singleton ValidateStruct uses --
// so it sees the same registered custom validators (phone, birthday,
// safeurl, ...) plus the validator library's built-ins (min, max, oneof,
// uri, ...). This is the primitive definition-driven FieldValue
// validation (services/custom_field_service.go) needs: ValidateStruct only
// works against a whole tagged Go struct known at compile time, but a custom
// field's validation rule comes from a FieldDefinition row at runtime, not a
// struct tag.
func ValidateVar(value interface{}, tag string) bool {
	return validate.Var(value, tag) == nil
}

// GetValidated retrieves and type-asserts the validated struct from context.
// This provides type-safe access to data set by ValidateJSONMiddleware.
func GetValidated[T any](c *gin.Context) (*T, *apperrors.AppError) {
	validated, exists := c.Get("validated")
	if !exists {
		return nil, apperrors.ErrInvalidInput("request", "validation data not found")
	}
	result, ok := validated.(*T)
	if !ok {
		return nil, apperrors.ErrInvalidInput("request", "invalid validation data type")
	}
	return result, nil
}

// ValidateJSONMiddleware validates JSON request body against a struct
// Usage: router.POST("/endpoint", ValidateJSONMiddleware(&MyStruct{}), handler)
func ValidateJSONMiddleware(template interface{}) gin.HandlerFunc {
	// Get the type of the template to create new instances per request
	templateType := reflect.TypeOf(template)
	if templateType.Kind() == reflect.Pointer {
		templateType = templateType.Elem()
	}

	return func(c *gin.Context) {
		// Create a new instance of the struct for each request
		obj := reflect.New(templateType).Interface()

		if err := c.ShouldBindJSON(obj); err != nil {
			logger.FromContext(c).Warn().Err(err).Msg("Invalid JSON in request body")

			appErr := apperrors.ErrInvalidInput("request body", err.Error())
			apperrors.AbortWithError(c, appErr)
			return
		}

		// Validate the struct
		if validationErrors := ValidateStruct(obj); len(validationErrors) > 0 {
			logger.FromContext(c).Warn().Interface("validation_errors", validationErrors).Msg("Validation failed")

			// Build detailed validation error
			appErr := apperrors.ErrValidation("Request validation failed")
			for _, ve := range validationErrors {
				appErr.WithDetails(ve.Field, ve.Message)
			}
			apperrors.AbortWithError(c, appErr)
			return
		}

		// Store validated object in context
		c.Set("validated", obj)
		c.Next()
	}
}
