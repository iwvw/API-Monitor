# Implementation Plan - Docker Page Styling Fixes

The goal is to resolve display and alignment issues on the Docker management page within the API Monitor application. The current layout suffers from column misalignment and content crowding due to insufficient Flexbox constraints.

## Proposed Changes

### CSS Styling (`src/css/server.css`)

1.  **Refine Docker Resource Table**:
    - Add specific rules for direct children (`span`) of `.docker-resource-header` and `.docker-resource-row` to ensure consistent alignment.
    - Implement `min-width: 0` and `overflow: hidden` on flex children to allow text truncation (`text-overflow: ellipsis`) to work correctly.
    - Reduce vertical padding slightly for a more compact view.

2.  **Fix Action Column**:
    - Explicitly set `flex-shrink: 0` and a fixed width/basis for the actions column to prevent it from collapsing or wrapping.
    - Restore the commented-out width constraint.

3.  **Enhance Visual Hierarchy**:
    - Add subtle separator lines or background variances if needed to distinguish rows better.
    - Ensure status badges and buttons are vertically centered.

4.  **Mobile Responsiveness**:
    - Add a media query to handle the table on smaller screens (e.g., allow horizontal scroll or stack content).

## Verification Plan

### Automated Checks
- Verify `src/css/server.css` syntax validity.

### Manual Verification
- Since I cannot run the frontend visually, I will rely on the code structure correctness.
- ensuring `flex` properties match the `server.html` inline styles mechanism.
- Verify that `text-overflow: ellipsis` is applicable to the containers.

## Completion Status
- [x] Docker Resource Table Styling
- [x] Action Column Fixing
- [x] Padding Adjustment
