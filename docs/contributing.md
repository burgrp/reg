# Contributing to Registry

Thank you for considering contributing to Registry! This document provides guidelines for contributing.

## How to Contribute

### Reporting Bugs

Before creating bug reports, please check existing issues to avoid duplicates.

When reporting bugs, include:

- Clear and descriptive title
- Steps to reproduce the behavior
- Expected behavior
- Actual behavior
- Environment details (OS, Go version, Registry version)
- Relevant logs or error messages

Example:

```
Title: Consumer long-poll returns immediately when wait parameter is set

Steps to reproduce:
1. Start registry server: ./reg serve
2. Make consumer request: curl "http://localhost:8080/consumer?name=temp&wait=5s"
3. Observe request returns in <1s instead of waiting

Expected: Request should wait up to 5 seconds
Actual: Request returns immediately

Environment:
- OS: Ubuntu 22.04
- Go: 1.21.0
- Registry: v1.0.0

Error logs:
[none]
```

### Suggesting Features

Feature requests are welcome! Please include:

- Clear description of the feature
- Use case or problem it solves
- Proposed solution or implementation approach
- Alternative solutions considered

### Pull Requests

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass: `go test ./...`
6. Format code: `go fmt ./...`
7. Commit with clear message
8. Push to your fork
9. Open pull request

## Development Guidelines

### Code Quality

- Write clean, readable code
- Add comments for complex logic
- Follow Go best practices and idioms
- Keep functions small and focused
- Avoid premature optimization

### Testing

- Add unit tests for new functions
- Add integration tests for new features
- Ensure tests are deterministic
- Use table-driven tests where appropriate
- Aim for reasonable test coverage

Example test:

```go
func TestMyFeature(t *testing.T) {
    // Setup
    client := NewClient("http://localhost:8080")

    // Execute
    result, err := client.MyFeature()

    // Verify
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### Documentation

- Update README.md for user-facing changes
- Update relevant docs in `docs/` directory
- Add godoc comments for exported types and functions
- Include examples in documentation
- Update CLAUDE.md for architectural changes

### Commit Messages

Use clear, descriptive commit messages:

```
Add feature to filter registers by metadata

- Implement metadata filtering in core registry
- Add query parameter support to REST API
- Update client library with filter option
- Add tests for filter functionality
```

Format:
- First line: imperative summary (50 chars or less)
- Blank line
- Detailed description (wrap at 72 chars)
- Reference issues: "Fixes #123"

## Code Review Process

### For Contributors

1. Submit pull request with clear description
2. Respond to review comments promptly
3. Make requested changes
4. Update PR description if scope changes

### For Reviewers

Focus on:

- Correctness: Does it work as intended?
- Tests: Are new features tested?
- Code quality: Is it readable and maintainable?
- Documentation: Are changes documented?
- Breaking changes: Are they necessary and documented?

Be constructive and respectful in feedback.

## Project Structure

Understand where changes belong:

```
cmd/           - CLI commands (user interface)
internal/      - Private implementation (not importable)
  registry/    - Core business logic (protocol-agnostic)
  rest/        - REST protocol layer
pkg/           - Public libraries (importable by other projects)
  client/      - Client interface and implementations
  wire/        - Low-level protocol clients
docs/          - Documentation
```

When adding features:

- Core logic goes in `internal/registry/`
- Protocol specifics go in `internal/rest/` or other protocol packages
- Client features go in `pkg/client/`
- CLI commands go in `cmd/`

## Testing Guidelines

### Unit Tests

Test individual functions in isolation:

```go
func TestRegistry_SetRegister(t *testing.T) {
    r := registry.New()
    err := r.SetRegister(context.Background(), "test", 123, nil, 10*time.Second)
    if err != nil {
        t.Fatal(err)
    }

    registers := r.WaitForChange(context.Background(), []string{"test"}, 0)
    if registers["test"].Value != 123 {
        t.Errorf("expected 123, got %v", registers["test"].Value)
    }
}
```

### Integration Tests

Test components working together:

```go
func TestClientServer_Integration(t *testing.T) {
    // Start test server
    server := httptest.NewServer(handler)
    defer server.Close()

    // Create client
    client := rest.NewClient(server.URL)

    // Test full flow
    ctx := context.Background()
    values, _, err := client.Consume(ctx, "test")
    // ... test logic
}
```

### Test Organization

- Place tests in same package as code being tested
- Use `_test.go` suffix for test files
- Group related tests in same file
- Use subtests for variations: `t.Run("subtest", func(t *testing.T) {...})`

## Performance Considerations

### When to Optimize

- Only after profiling shows bottleneck
- Document why optimization is needed
- Keep unoptimized version in comments if complex

### What to Optimize

Focus on:
- Reducing allocations in hot paths
- Efficient use of channels and goroutines
- Appropriate buffer sizes
- Minimizing lock contention

Avoid:
- Premature optimization
- Micro-optimizations without benchmarks
- Complex code for marginal gains

### Benchmarking

Add benchmarks for performance-critical code:

```go
func BenchmarkRegistry_SetRegister(b *testing.B) {
    r := registry.New()
    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r.SetRegister(ctx, "test", i, nil, 10*time.Second)
    }
}
```

## Breaking Changes

Avoid breaking changes when possible. If necessary:

1. Discuss in issue before implementing
2. Document clearly in PR description
3. Update version following semantic versioning
4. Provide migration guide
5. Consider deprecation period

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Questions?

- Open an issue for questions about contributing
- Check existing issues and pull requests
- Read the documentation in `docs/`

## Recognition

Contributors will be acknowledged in:
- Release notes
- Contributors list (if maintained)

Thank you for contributing to Registry!
