# Design Principles for Enterprise UI

This framework establishes a quality standard for crafted, minimal design inspired by Linear, Notion, and Stripe.

## Key Foundation Steps

Before coding, commit to a design direction by considering:
- **Product context** — What emotional job does the interface need to do?
- **User type** — Do they need density or guidance?
- **Personality choice** — Precision, warmth, sophistication, boldness, utility, or data-focus?
- **Color foundation** — Warm, cool, pure, or tinted neutrals?
- **Layout approach** — Dense grids, generous spacing, sidebar, or split panels?

## Core Craft Principles

**Spacing Grid**: All measurements use 4px increments (4px, 8px, 12px, 16px, 24px, 32px).

**Padding Symmetry**: "TLBR must match. If top padding is 16px, left/bottom/right must also be 16px."

**Border Radius**: Choose a system (sharp: 4-8px, soft: 8-12px, minimal: 2-4px) and maintain consistency.

**Depth Strategy**: Pick one approach—borders-only, subtle single shadows, layered shadows, or surface color shifts—and commit to it across the interface.

**Control Treatment**: Custom components deserve container styling; avoid native form elements.

**Data Styling**: Use monospace with tabular-nums for numbers, codes, and timestamps.

**Color Discipline**: Reserve color for meaning (status, actions, errors); build structure with gray.

## Navigation & Context

Ground screens with navigation, location indicators, and user context. Consider using the same background for sidebars as content areas, separated by subtle borders rather than color shifts.

This approach prioritizes intentional design decisions over defaults, ensuring every pixel reflects the product's specific needs.
