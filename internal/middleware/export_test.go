package middleware

// SupportEntityForTest exposes supportEntity to the external tests.
func SupportEntityForTest(method string, req any) (string, string) { return supportEntity(method, req) }
