# Filepicker Search Implementation - Notes

## Goal
Add `/` key functionality in filepicker (Ctrl+O) to filter/search files by name, similar to how `/` works in the main TUI textview.

## Requirements
- Press `/` to activate search mode
- Live case-insensitive filtering
- `../` (parent directory) always visible
- Show "No matching files" when nothing matches
- Esc to cancel (return to main app for sending messages)
- Enter to confirm search and close search input

## Approaches Tried

### Approach 1: Modify Flex Layout In-Place
Add search input to the existing flex container by replacing listView with searchInput.

**Issues:**
- tview's `RemoveItem`/`AddItem` causes UI freezes/hangs
- Using `app.QueueUpdate` or `app.Draw` didn't help
- Layout changes don't render properly

### Approach 2: Add Input Capture to ListView
Handle `/` key in listView's SetInputCapture.

**Issues:**
- Key events don't reach listView when filepicker is open
- Global app input capture handles `/` for main textview search first
- Even when checking `pages.GetFrontPage()`, the key isn't captured

### Approach 3: Global Handler with Page Replacement
Handle `/` in global app input capture when filepicker page is frontmost.

**Issues:**
- Search input appears but text is invisible (color issues)
- Enter/Esc not handled - main TUI captures them
- Creating new pages adds on top instead of replacing, causing split-screen effect
- Enter on file item opens new filepicker (page stacking issue)

### Approach 4: Overlay Page (Modal-style)
Create a new flex with search input on top and filepicker below, replace the page.

**Issues:**
- Page replacement causes split-screen between main app and filepicker
- Search input renders but invisible text
- Enter/Esc handled by main TUI, not search input
- State lost when recreating filepicker

## Root Causes

1. **tview UI update issues**: Direct manipulation of flex items causes freezes or doesn't render
2. **Input capture priority**: Even with page overlay, main TUI's global input capture processes keys first
3. **Esc key conflict**: Esc is used for sending messages in main TUI, and it's hard to distinguish when filepicker is open
4. **Focus management**: tview's focus system doesn't work as expected with dynamic layouts

## Possible Solutions (Not Tried)

1. **Use tview's built-in Filter method**: ListView has a SetFilterFunc that might work
2. **Create separate search primitive**: Instead of replacing list, use a separate text input overlay
3. **Different key for search**: Use a key that isn't already mapped in main TUI
4. **Fork/extend tview**: May need to modify tview itself for better dynamic UI updates
5. **Use form with text input**: tview.Forms might handle input better

## Current State
All search-related changes rolled back. Filepicker works as before without search functionality.
