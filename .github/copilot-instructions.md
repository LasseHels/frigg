# General

- We prefer simple, clean, maintainable solutions over clever or complex ones, even if the latter are more concise or performant. Readability and maintainability are primary concerns.
- Make the smallest reasonable changes to get to the desired outcome. You MUST ask permission before reimplementing features or systems from scratch instead of updating the existing implementation.
- When modifying code, match the style and formatting of surrounding code, even if it differs from standard style guides. Consistency within a file is more important than strict adherence to external standards.
- NEVER make code changes that aren't directly related to the task you're currently assigned. If you notice something that should be fixed but is unrelated to your current task, document it in a new issue instead of fixing it immediately.
- NEVER add code comments unless I explicitly ask you to.
- When you are trying to fix a bug or compilation error or any other issue, YOU MUST NEVER throw away the old implementation and rewrite without explicit permission from the user. If you are going to do this, YOU MUST STOP and get explicit permission from the user.
- NEVER name things as "improved" or "new" or "enhanced", etc. Code naming should be evergreen. What is new today will be "old" someday.
- Always pin versions. Avoid the use of `latest` tags.
- Use British English when writing in a natural language.
- ALWAYS run linting and tests before committing code.

# Testing

- Tests MUST cover the functionality being implemented.
- NEVER ignore the output of the system or the tests. Logs and messages often contain CRITICAL information.
- TEST OUTPUT MUST BE PRISTINE TO PASS.
- If the logs are supposed to contain errors, capture and test them.

# Golang

- ALWAYS adhere to [Google](https://google.github.io/styleguide/go/) and
  [Uber](https://github.com/uber-go/guide/blob/master/style.md)'s style guides for Go as well as
  [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).
- PREFER to mark tests as parallel.
- PREFER to write tests using a table-driven structure where test cases are defined in a map.
- The `make lint` command is used to lint code.
- The `make test-all` command is used to test code.
- PREFER to avoid global state.
- ALWAYS handle errors.
- ALWAYS capitalise log messages.
- NEVER capitalise error messages.
- PREFER `slog` for logging, unless something else has already been set up in the repository.
- PREFER the [early return pattern](https://danp.net/posts/reducing-go-nesting) to reduce nesting:
```go
    // Bad.
    if len(paths) == 1 {
        return workflowWithGoMod(paths[0])
    } else {
        return workflowWithMultiGoMod(paths)
    }


    // Good.
    if len(paths) == 1 {
        return workflowWithGoMod(paths[0])
    }

    return workflowWithMultiGoMod(paths)
```
- NEVER change the Go version used by a project without explicit permission from the user.
- NEVER use anonymous structs like this:
```go
	data := struct {
		GoModPath        string
		WorkingDirectory string
	}{
		GoModPath: goModPath,
	}
```
- ALWAYS stick to the principle of least visibility. NEVER make something public if it is only used in the package where
  it is defined.
- ALWAYS end comments with a period:
```go
// Bad.
// If there are no hits, we know for certain the repo doesn't use private modules

// Good.
// If there are no hits, we know for certain the repo doesn't use private modules.
```
- PREFER positive conditionals over negative ones:
```go
// Bad.
if !isMultiModule {
    goModPath = paths[0]
    l.Info("Found go.mod file", slog.String("path", goModPath))
} else {
    l.Info("Found multiple go.mod files", slog.Int("count", len(paths)))
}

// Good.
if isMultiModule {
    l.Info("Found multiple go.mod files", slog.Int("count", len(paths)))
} else {
    goModPath = paths[0]
    l.Info("Found go.mod file", slog.String("path", goModPath))
}
```
- NEVER use `time.Sleep` and `time.Ticker` in tests. Use a poll approach instead to avoid unnecessary delays:
```go
// Bad.
time.Sleep(3 * time.Second)

// Bad.
ticker := time.NewTicker(time.Second)

// Good.
assert.EventuallyWithT(t, func(collect *assert.CollectT) {
	// Some check.
}, time.Minute, time.Second)
```
- PREFER to simplify `if`-statements to contain only a single condition:
```go
// Bad.
for _, line := range lines {
    trimmedLine := strings.TrimSpace(line)
    if strings.HasPrefix(trimmedLine, "module ") {
        moduleLineFound = true
        continue
    }

    if moduleLineFound && strings.Contains(trimmedLine, "github.com/Maersk-Global/") {
        return true, nil
    }
}

// Good.
for _, line := range lines {
    trimmedLine := strings.TrimSpace(line)
    if strings.HasPrefix(trimmedLine, "module ") {
        moduleLineFound = true
        continue
    }

    if !moduleLineFound {
        continue
    }

    if strings.Contains(trimmedLine, "github.com/Maersk-Global/") {
        return true, nil
    }
}
```
- ALWAYS wrap deferred function calls that can fail in an anonymous function to clearly show that an error is being ignored:
```go
// Bad.
defer resp.Body.Close()

// Good.
defer func() {
    _ = resp.Body.Close()
}()
```
- ALWAYS lowercase test case names:
```
// Bad.
t.Run("Health endpoint returns healthy", func(t *testing.T) {

})

// Good.
t.Run("health endpoint returns healthy", func(t *testing.T) {

})
```

# GitHub Actions

- PREFER to pin action dependencies with commit SHA rather than git tag. A tag is mutable and can be changed by a
  malicious actor to point to a compromised commit (this happened in the
  [`tj-actions/changed-files`](https://github.com/tj-actions/changed-files/issues/2464#issuecomment-2726055302) security breach).
- PREFER for actions to run often, ideally on every commit. When possible, avoid creating actions that only run on specific branches or when specific files change.
