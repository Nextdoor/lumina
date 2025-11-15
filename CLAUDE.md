# Claude Code Instructions for Lumina

This file contains project-specific instructions for Claude Code when working on the Lumina repository.

## Project Context

This repository is currently in active internal development but will be released as a public open-source project. All code and documentation must be written with this future in mind.

## Code Quality Standards

### 1. Open Source Readiness

**CRITICAL**: This project will be open-sourced. All code, comments, documentation, and configuration must:
- NOT contain Nextdoor-specific internal references, URLs, or domain names
- NOT include internal service names, hostnames, or infrastructure details
- NOT reference internal tools, systems, or processes specific to Nextdoor
- Use generic examples and placeholder values instead of real internal data
- Be written as if the code is already public

Before committing any code:
1. Review all comments for internal references
2. Check configuration files for hardcoded internal values
3. Verify examples and documentation use generic/placeholder data
4. Ensure error messages don't leak internal information

### 2. Code Coverage Requirements

**100% code coverage is mandatory** for all code in this repository.

Requirements:
- All packages must maintain 100% test coverage
- Use `// coverage:ignore` comments ONLY when 100% coverage is genuinely not reasonable
- Every `// coverage:ignore` must have a clear comment explaining why coverage is not possible
- CI/CD must fail if coverage drops below 100%
- When adding new code, tests must be included in the same commit/PR

Valid reasons for `// coverage:ignore`:
- Pure data structures with no logic (e.g., type definitions)
- Unreachable error conditions in generated code
- Defensive programming checks that cannot be triggered in tests
- Platform-specific code that cannot be tested in CI environment

Invalid reasons:
- "Hard to test" - refactor the code to make it testable
- "Takes too long" - optimize the test or use appropriate mocking
- "Edge case" - edge cases must be tested

**Do NOT write tests for pure data structures**: Testing that struct field assignment works (e.g., `config.Field = "value"`) provides zero value. These types are covered through their usage in real tests.

### 3. Testing Strategy

**Integration tests are a primary focus** of this project.

Testing requirements:
- **Unit tests**: Test individual functions and methods in isolation
- **Integration tests**: Test component interactions and real workflows
  - Integration tests should test actual behavior, not mocked behavior
  - Should cover realistic end-to-end scenarios
  - Should validate error handling and edge cases
- **Table-driven tests**: Use Go's table-driven test pattern for multiple scenarios
- **Test organization**:
  - Unit tests in `*_test.go` files alongside source
  - Integration tests in `integration_test.go` or separate `integration/` directory
- **Test naming**: Use descriptive test names that explain what is being tested

Integration test priorities:
1. Core functionality and happy paths
2. Error conditions and failure modes
3. Boundary conditions and edge cases
4. Concurrent access patterns (if applicable)
5. Performance characteristics (where relevant)

## Development Workflow

### Pull Requests

- Follow conventional commit format for PR titles: `type(component): description`
- Open PRs in draft mode initially
- Include comprehensive descriptions explaining changes
- Reference related issues or tickets
- Ensure all CI checks pass (including coverage) before requesting review

### Commit Messages

- Use conventional commits format
- Always include a component value: `feat(api): add new endpoint`
- Valid types: feat, fix, docs, test, refactor, chore, ci
- Be specific about what changed and why

### Pre-Commit Checklist

**MANDATORY**: Before every commit, run these commands in order:

```bash
# 1. Run the linter to catch style issues
make lint

# 2. Run all tests with race detection
go test -race ./...

# 3. If both pass, stage and commit
git add <files>
git commit -m "your message"
```

If either the linter or tests fail:
- Fix the issues
- Re-run both checks
- Only commit when both pass

**Never skip these checks**. CI will fail if linting or tests fail, and you'll need to amend your commit anyway.

## Code Review Checklist

Before submitting code for review:
- [ ] No Nextdoor-specific references or internal data
- [ ] 100% code coverage (or justified coverage:ignore comments)
- [ ] Integration tests included for new functionality
- [ ] All tests pass locally
- [ ] Code follows Go best practices and project conventions
- [ ] Documentation updated (if applicable)
- [ ] Error messages are generic and don't leak internal info

## When Adding New Features

1. Write integration tests first (TDD approach encouraged)
2. Implement the feature with unit tests
3. Verify 100% coverage
4. Run full test suite including integration tests
5. Check for any internal references that need to be genericized
6. Update documentation

## CI/CD Expectations

The CI pipeline must enforce:
- Code coverage at 100% (fail if below)
- All tests pass (unit + integration)
- Linting passes
- No hardcoded internal references (future enhancement)
- Build succeeds

## Documentation Style

### README Files

README files should be **concise and information-dense**:

- **Be terse**: Get to the point quickly, avoid fluff and repetition
- **Minimal examples**: 1-2 short code snippets maximum
- **Focus on "what" and "why"**: Not extensive "how-to" tutorials
- **Bullet points over paragraphs**: Easy to scan
- **No dozens of code examples**: Link to godoc or tests for detailed usage

Bad README:
```markdown
## How to Create a Client

First, you'll need to create a client configuration. Here's how...
[10 paragraphs of explanation]
[5 different code examples showing every possible option]
```

Good README:
```markdown
## Quick Start

`client := aws.NewClient(aws.ClientConfig{DefaultRegion: "us-west-2"})`

See godoc for full API reference.
```

### Code Comments

In contrast to READMEs, **code comments should be verbose and explain intent**:

```go
// Good: Explains WHY, not just WHAT
// We need to query all regions because Savings Plans can apply to instances
// in any region (for Compute SPs) or specific regions (for EC2 Instance SPs).
// Querying all regions upfront is more efficient than per-region queries
// because the AWS API charges per request, not per result count.
regions := []string{"us-west-2", "us-east-1", "eu-west-1"}

// Bad: Just repeats the code
// Set regions variable
regions := []string{"us-west-2", "us-east-1", "eu-west-1"}
```

Key principles for code comments:
- Explain **intent and reasoning**, not mechanics
- Document **why decisions were made**
- Call out **non-obvious implications**
- Explain **edge cases and gotchas**
- Reference **RFC sections or external docs** when relevant

## Questions or Exceptions

If you need to deviate from these guidelines, always:
1. Ask the user first
2. Document the reason in code comments
3. Create a TODO/FIXME if it needs to be addressed before open-sourcing
