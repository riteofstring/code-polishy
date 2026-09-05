package repository

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

func parsePythonFileURL(value string) (string, error) {
	if hasPythonDynamicValue(value) {
		return "", fmt.Errorf("file URL contains a dynamic substitution")
	}
	relative := value[len("file:"):]
	if relative == "" || strings.ContainsAny(relative, "?#%") {
		return "", fmt.Errorf("file URL must name one relative path")
	}
	if strings.ContainsAny(relative, "\r\n") {
		return "", fmt.Errorf("file URL must name one relative path")
	}
	return relative, nil
}

func parsePythonDirectGitURL(value string) (PythonGitSource, error) {
	withoutQuery, fragment, fragmentFound, err := pythonDirectGitURLParts(value)
	if err != nil {
		return PythonGitSource{}, err
	}
	separator := strings.LastIndex(withoutQuery, "@")
	if separator < 0 {
		return PythonGitSource{}, fmt.Errorf("git URL must end in one declared revision")
	}
	reference, err := pythonGitReference(withoutQuery[separator+1:])
	if err != nil {
		return PythonGitSource{}, err
	}
	source, err := parsePythonGitRepositoryURL(withoutQuery[:separator], true)
	if err != nil {
		return PythonGitSource{}, err
	}
	if fragmentFound {
		source.Subdirectory, err = parsePythonGitFragment(fragment)
		if err != nil {
			return PythonGitSource{}, err
		}
	}
	if pythonFullCommit(reference) {
		source.Commit = reference
	}
	source.DeclaredRef = reference
	return source, nil
}

func pythonDirectGitURLParts(value string) (string, string, bool, error) {
	if hasPythonDynamicValue(value) {
		return "", "", false, fmt.Errorf("git URL contains a dynamic substitution")
	}
	withoutFragment, fragment, fragmentFound := strings.Cut(value, "#")
	if fragmentFound && strings.Contains(fragment, "#") {
		return "", "", false, fmt.Errorf("git URL contains an unknown fragment")
	}
	withoutQuery, query, queryFound := strings.Cut(withoutFragment, "?")
	if queryFound {
		if pythonURLHasCredential(query) {
			return "", "", false, fmt.Errorf("git URL query contains credentials")
		}
		return "", "", false, fmt.Errorf("git URL query is unsupported")
	}
	return withoutQuery, fragment, fragmentFound, nil
}

func ParsePythonGitSource(value, commit, subdirectory string) (PythonGitSource, error) {
	source, err := ParsePythonGitInventorySource(value, commit, subdirectory)
	if err != nil {
		return PythonGitSource{}, err
	}
	if err := source.ValidateExactPin(); err != nil {
		return PythonGitSource{}, err
	}
	return source, nil
}

func ParsePythonGitInventorySource(value, commit, subdirectory string) (PythonGitSource, error) {
	if hasPythonDynamicValue(value) || hasPythonDynamicValue(subdirectory) {
		return PythonGitSource{}, fmt.Errorf("git lock source contains a dynamic substitution")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return PythonGitSource{}, fmt.Errorf("git lock source URL is unsupported")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return parsePythonUVGitSource(parsed, commit, subdirectory)
	}
	if !pythonFullCommit(commit) {
		return PythonGitSource{}, fmt.Errorf("git lock source must provide one full 40-character commit SHA")
	}
	source, err := parsePythonGitRepositoryURL(value, false)
	if err != nil {
		return PythonGitSource{}, err
	}
	normalized, err := normalizePythonSubdirectory(subdirectory)
	if err != nil {
		return PythonGitSource{}, err
	}
	source.DeclaredRef = strings.ToLower(commit)
	source.Commit = strings.ToLower(commit)
	source.Subdirectory = normalized
	return source, nil
}

func parsePythonUVGitSource(parsed *url.URL, suppliedCommit, suppliedSubdirectory string) (PythonGitSource, error) {
	values, err := pythonUVGitQuery(parsed)
	if err != nil {
		return PythonGitSource{}, err
	}
	declaredRef, resolvedCommit, err := pythonUVGitCommit(values, parsed.Fragment, suppliedCommit)
	if err != nil {
		return PythonGitSource{}, err
	}
	normalizedSubdirectory, err := pythonUVGitSubdirectory(values, suppliedSubdirectory)
	if err != nil {
		return PythonGitSource{}, err
	}
	source, err := parsePythonGitRepositoryURL(pythonUVGitRepositoryURL(parsed), false)
	if err != nil {
		return PythonGitSource{}, err
	}
	source.DeclaredRef = declaredRef
	for _, kind := range []string{"rev", "tag", "branch"} {
		if len(values[kind]) != 0 {
			source.RefKind = kind
		}
	}
	source.Commit = resolvedCommit
	source.Subdirectory = normalizedSubdirectory
	return source, nil
}

func pythonUVGitQuery(parsed *url.URL) (url.Values, error) {
	if pythonURLHasCredential(parsed.RawQuery) || pythonURLHasCredential(parsed.Fragment) {
		return nil, fmt.Errorf("git lock source contains credentials")
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("git lock source query is malformed")
	}
	if !pythonUVGitQueryKeys(values) {
		return nil, fmt.Errorf("git lock source query is unsupported")
	}
	return values, nil
}

func pythonUVGitQueryKeys(values url.Values) bool {
	for key := range values {
		if key != "rev" && key != "tag" && key != "branch" && key != "subdirectory" {
			return false
		}
	}
	return true
}

func pythonUVGitCommit(values url.Values, fragment, supplied string) (string, string, error) {
	declaredRef, err := pythonUVGitReference(values)
	if err != nil {
		return "", "", err
	}
	if !pythonFullCommit(fragment) {
		return "", "", fmt.Errorf("git lock source must resolve to one full 40-character commit SHA")
	}
	resolvedCommit := strings.ToLower(fragment)
	if pythonFullCommit(declaredRef) && declaredRef != resolvedCommit {
		return "", "", fmt.Errorf("git lock source declared ref differs from its resolved commit")
	}
	if !pythonUVGitSuppliedCommit(supplied, resolvedCommit) {
		return "", "", fmt.Errorf("git lock source field commit differs from its resolved commit")
	}
	return declaredRef, resolvedCommit, nil
}

func pythonUVGitSuppliedCommit(supplied, resolved string) bool {
	if supplied == "" {
		return true
	}
	return pythonFullCommit(supplied) && strings.EqualFold(supplied, resolved)
}

func pythonUVGitSubdirectory(values url.Values, supplied string) (string, error) {
	encoded, err := pythonUVGitEncodedSubdirectory(values["subdirectory"])
	if err != nil {
		return "", err
	}
	normalized, err := normalizePythonSubdirectory(supplied)
	if err != nil {
		return "", err
	}
	if encoded != "" && normalized != "" && encoded != normalized {
		return "", fmt.Errorf("git lock source subdirectory differs from its encoded subdirectory")
	}
	if encoded != "" {
		return encoded, nil
	}
	return normalized, nil
}

func pythonUVGitEncodedSubdirectory(values []string) (string, error) {
	if len(values) > 1 {
		return "", fmt.Errorf("git lock source declares subdirectory more than once")
	}
	if len(values) == 0 {
		return "", nil
	}
	return normalizePythonSubdirectory(values[0])
}

func pythonUVGitRepositoryURL(parsed *url.URL) string {
	repositoryURL := *parsed
	repositoryURL.RawQuery = ""
	repositoryURL.ForceQuery = false
	repositoryURL.Fragment = ""
	repositoryURL.RawFragment = ""
	return repositoryURL.String()
}

func parsePythonGitRepositoryURL(value string, direct bool) (PythonGitSource, error) {
	parsed, err := pythonGitRepositoryURL(value)
	if err != nil {
		return PythonGitSource{}, err
	}
	scheme, err := pythonGitRepositoryScheme(parsed.Scheme, direct)
	if err != nil {
		return PythonGitSource{}, err
	}
	if err := pythonGitRepositoryHost(parsed); err != nil {
		return PythonGitSource{}, err
	}
	user, err := pythonGitRepositoryUser(parsed, scheme)
	if err != nil {
		return PythonGitSource{}, err
	}
	cleaned, err := pythonGitRepositoryPath(parsed)
	if err != nil {
		return PythonGitSource{}, err
	}
	return PythonGitSource{Scheme: scheme, Host: strings.ToLower(parsed.Host), Path: cleaned, User: user}, nil
}

func pythonGitRepositoryURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || !pythonGitRepositoryShape(parsed) {
		return nil, fmt.Errorf("git repository URL is malformed")
	}
	return parsed, nil
}

func pythonGitRepositoryShape(parsed *url.URL) bool {
	return parsed.Scheme != "" && parsed.Host != "" && parsed.Opaque == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func pythonGitRepositoryScheme(scheme string, direct bool) (string, error) {
	scheme = strings.ToLower(scheme)
	if direct {
		return pythonDirectGitRepositoryScheme(scheme)
	}
	return pythonLockedGitRepositoryScheme(scheme)
}

func pythonDirectGitRepositoryScheme(scheme string) (string, error) {
	if scheme != "git+https" && scheme != "git+ssh" {
		return "", fmt.Errorf("git URL must use git+https:// or git+ssh://")
	}
	return strings.TrimPrefix(scheme, "git+"), nil
}

func pythonLockedGitRepositoryScheme(scheme string) (string, error) {
	scheme = strings.TrimPrefix(scheme, "git+")
	if scheme != "https" && scheme != "ssh" {
		return "", fmt.Errorf("git lock source must use HTTPS or SSH")
	}
	return scheme, nil
}

func pythonGitRepositoryHost(parsed *url.URL) error {
	if hasPythonDynamicValue(parsed.Host) {
		return fmt.Errorf("git repository URL contains a dynamic substitution")
	}
	if hasPythonDynamicValue(parsed.Path) || strings.ContainsAny(parsed.Host, " \t\r\n") {
		return fmt.Errorf("git repository URL contains a dynamic substitution")
	}
	return nil
}

func pythonGitRepositoryUser(parsed *url.URL, scheme string) (string, error) {
	if parsed.User == nil {
		return "", nil
	}
	if scheme == "https" {
		return "", fmt.Errorf("https git URL must not include user information")
	}
	user := parsed.User.Username()
	if _, password := parsed.User.Password(); password || user == "" || hasPythonDynamicValue(user) {
		return "", fmt.Errorf("ssh git URL must not include a password or dynamic user")
	}
	return user, nil
}

func pythonGitRepositoryPath(parsed *url.URL) (string, error) {
	if !pythonGitRepositoryPathShape(parsed) {
		return "", fmt.Errorf("git repository path is ambiguous")
	}
	cleaned := path.Clean(parsed.Path)
	if cleaned != parsed.Path || cleaned == "/" || strings.HasPrefix(cleaned, "/../") {
		return "", fmt.Errorf("git repository path is not normalized")
	}
	return cleaned, nil
}

func pythonGitRepositoryPathShape(parsed *url.URL) bool {
	if parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") {
		return false
	}
	if strings.Contains(parsed.Path, "@") {
		return false
	}
	return !strings.Contains(parsed.EscapedPath(), "%")
}

func parsePythonGitFragment(fragment string) (string, error) {
	if hasPythonDynamicValue(fragment) {
		return "", fmt.Errorf("git URL fragment contains a dynamic substitution")
	}
	if pythonURLHasCredential(fragment) {
		return "", fmt.Errorf("git URL fragment contains credentials")
	}
	values, err := url.ParseQuery(fragment)
	if err != nil {
		return "", fmt.Errorf("git URL fragment is malformed")
	}
	if len(values) != 1 || len(values["subdirectory"]) != 1 {
		return "", fmt.Errorf("git URL fragment must contain only one subdirectory")
	}
	return normalizePythonSubdirectory(values["subdirectory"][0])
}

func normalizePythonSubdirectory(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value != strings.TrimSpace(value) || strings.Contains(value, "\\") || hasPythonDynamicValue(value) {
		return "", fmt.Errorf("git subdirectory is malformed")
	}
	cleaned := path.Clean(value)
	if path.IsAbs(value) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value {
		return "", fmt.Errorf("git subdirectory must be a normalized relative path")
	}
	return cleaned, nil
}

func pythonURLHasCredential(value string) bool {
	values, err := url.ParseQuery(value)
	if err != nil {
		return true
	}
	for key := range values {
		if pythonCredentialQueryKey(key) {
			return true
		}
	}
	return false
}

func pythonCredentialQueryKey(key string) bool {
	key = strings.ToLower(key)
	if key == "key" || key == "user" || key == "username" {
		return true
	}
	for _, marker := range []string{"token", "password", "passwd", "secret", "credential", "auth"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func hasPythonDynamicValue(value string) bool {
	return strings.ContainsAny(value, "${}") || strings.Contains(value, "$(") || strings.Contains(value, "{{") || strings.Contains(value, "}}")
}

func pythonFullCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}
