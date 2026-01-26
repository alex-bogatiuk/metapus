---
name: ui-metadata-provisioning
description: Use when implementing metadata registries or handlers that serve descriptive data to the frontend for dynamic UI rendering.
---

# UI Metadata Provisioning in Metapus

This skill guides the implementation of the backend-to-frontend metadata bridge, enabling dynamic and responsive UI.

## Goal
To allow the Next.js frontend to automatically render forms, lists, and validations based on backend-provided metadata.

## Core Rules
1. **SINGLE SOURCE OF TRUTH**: Metadata provided to the UI must match the Go struct definitions.
2. **DESCRIBABLE WIDGETS**: Map Go types and tags to frontend components (e.g., `decimal` -> `MoneyInput`).
3. **VALIDATION SCHEMAS**: Provide constraints (min/max/regex) to the frontend for client-side validation.

## Instructions

### 1. Metadata Registry Integration
Register all entities in the `MetadataRegistry` during application startup (`cmd/server/main.go`).

### 2. Describing Fields
Metadata should include:
- `Name`: Internal field name.
- `Label`: Human-readable name.
- `Type`: Data type (string, uuid, decimal, etc.).
- `Widget`: Suggested UI component.
- `Validators`: Validation rules.

### 3. Dynamic Enumerations
Provide endpoints or embedded metadata for `Enums`, allowing the UI to populate dropdowns dynamically.

## Constraints
- **NO** hardcoding of field lists on the frontend for standardized objects.
- **Ensure** metadata is localized appropriately if multi-language support is required.
