# Issue Solving Workflow

This document provides guidelines for the autonomous issue solver agent.

## General Approach

### Phase 1: Understanding
1. Read the issue description thoroughly
2. Review any context files listed in the issue
3. Check acceptance criteria
4. Identify unclear parts that might need clarification

### Phase 2: Planning
1. Break down the implementation into logical steps
2. Identify files that need modification
3. Plan commit structure (what gets committed together)
4. Consider test requirements

### Phase 3: Implementation
1. Create a feature branch if not already done
2. Implement changes incrementally
3. Make logical commits (see Commit Guidelines below)
4. Write or update tests
5. Verify tests pass

### Phase 4: Completion
1. Review changes against acceptance criteria
2. Create pull request with clear description
3. Move issue to review status using `move_issue` tool

## Branch Naming

Use format: `fix/issue-{id}-{short-description}` or `feat/issue-{id}-{short-description}`

Examples:
- `fix/issue-42-login-timeout`
- `feat/issue-123-user-auth`
- `fix/issue-456-null-pointer`

**Do not use:**
- Generic names like `feature-branch` or `fix-1`
- Branch names longer than 60 characters
- Spaces or special characters (except hyphens)

## Commit Guidelines

### Commit Message Format
```
<type>(<scope>): <short description>

[optional body with more detail]
[optional footer with ticket reference]
```

### Types
- `fix` - Bug fix
- `feat` - New feature
- `refactor` - Code refactoring (no feature or fix)
- `test` - Adding or updating tests
- `docs` - Documentation changes
- `chore` - Maintenance tasks

### Examples
```bash
fix(auth): handle login timeout gracefully

Added proper error handling when the login request times out.
The user now sees a clear message instead of a generic error.

Closes #42
```

```bash
feat(auth): implement JWT token generation

- Added token generation on successful login
- Token refresh endpoint implemented
- Logout invalidates tokens
```

## Testing Requirements

- All new features must include tests
- Bug fixes should include a regression test
- Run existing tests before committing to ensure no breakage
- If tests don't exist for the area you're modifying, add them

## Creating Sub-Issues

If an issue is too complex or you discover related work that should be separate, use the `create_issue` tool to create sub-issues.

When creating a sub-issue:
1. Use a unique ID (e.g., `42-1`, `42-2` or just a new number)
2. Link it to the parent via `related_issues` field
3. Set appropriate priority
4. Consider if it needs to be completed before or after the parent

## Using PM Consultation

Call `pm_consult` when:
- You're unsure about the approach
- You've made significant progress and want feedback
- You're stuck on a problem for too long
- You need guidance on priorities
- Every ~75 tool calls (automatic PM check-in)

Don't be afraid to ask for help - the PM exists to keep you on track.

## Handling Errors

### Git Conflicts
1. Read the conflict markers
2. Understand both changes
3. Resolve to the correct state (or ask PM if unclear)
4. `git add` the resolved file
5. Continue work

### Test Failures
1. Read the test output
2. Understand what failed
3. Fix the issue or update the test if it's incorrect
4. Re-run tests until they pass

### Build Failures
1. Read the compiler error
2. Fix syntax or type errors
3. Ensure dependencies are available
4. Verify build succeeds

## Progress Tracking

Use `add_issue_comment` to track progress:
- Before starting a major task: "Starting implementation of X"
- After completing a phase: "Completed phase 1: authentication backend"
- When encountering blockers: "Blocked on: need API access to service Y"

## Guardrails

### Do
- Make small, focused commits
- Write descriptive commit messages
- Test your changes before committing
- Ask PM when unsure
- Keep the issue file updated with comments

### Don't
- Make huge commits that do too many things
- Push directly to main/master (create PRs only)
- Ignore failing tests
- Leave debugging code in production
- Use `git push --force` on shared branches

## Quick Reference

### Essential Commands
```bash
# Create and switch to feature branch
git checkout -b fix/issue-{id}-{description}

# Stage and commit
git add .
git commit -m "fix(scope): description"

# Push branch
git push -u origin HEAD

# Create PR (before using create_pr tool)
git push -u origin HEAD
# Then use create_pr tool
```

### Tool Reference
```
move_issue status=review    # When PR is ready
move_issue status=done      # When PR is merged
move_issue status=archive   # If abandoning the issue
create_issue id=X title="..." description="..."
pm_consult question="..."
add_issue_comment body="..."
```

## Common Patterns

### Fixing a Bug
1. Reproduce the bug
2. Identify the root cause
3. Implement fix
4. Add regression test
5. Commit with `fix:` prefix

### Adding a Feature
1. Understand requirements
2. Design the solution
3. Implement incrementally
4. Add tests
5. Commit with `feat:` prefix

### Refactoring
1. Ensure tests cover the code
2. Make small, safe changes
3. Run tests after each change
4. Commit with `refactor:` prefix

## Exit Criteria

The issue is complete when:
- All acceptance criteria are met
- Code is committed on a feature branch
- Tests pass
- PR is created
- Issue moved to `review` status

If you cannot complete the issue:
1. Document what was tried
2. Add comments to the issue explaining the blocker
3. Move issue to `archive` if abandoned
4. Call `pm_consult` to discuss