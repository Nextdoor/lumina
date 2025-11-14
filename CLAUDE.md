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
- Unreachable error conditions in generated code
- Defensive programming checks that cannot be triggered in tests
- Platform-specific code that cannot be tested in CI environment

Invalid reasons:
- "Hard to test" - refactor the code to make it testable
- "Takes too long" - optimize the test or use appropriate mocking
- "Edge case" - edge cases must be tested

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

## Questions or Exceptions

If you need to deviate from these guidelines, always:
1. Ask the user first
2. Document the reason in code comments
3. Create a TODO/FIXME if it needs to be addressed before open-sourcing
