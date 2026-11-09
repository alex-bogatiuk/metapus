# Contributing to Metapus

Thank you for your interest in contributing to Metapus! 🎉

We welcome contributions of all kinds — bug fixes, new features, documentation
improvements, and ideas. Please take a moment to review this guide before
submitting your contribution.

---

## ⚖️ Contributor License Agreement (CLA)

**Before your contribution can be accepted, you must agree to our
[Contributor License Agreement (CLA)](./CLA.md).**

This is required because Metapus uses a **dual-licensing model**:

- The community edition is licensed under **AGPL-3.0** (see [LICENSE](./LICENSE)).
- A commercial license is available for enterprises that cannot comply with AGPL.

The CLA ensures that the Maintainer can continue to offer both license options
while your code remains part of the open-source project. **You retain full
ownership of your contributions.**

### How to agree

Include the following statement in your **first Pull Request**:

> I have read the CLA and I agree to its terms.

Or add your name to the contributors table in [CLA.md](./CLA.md).

---

## 🚀 Getting Started

### Prerequisites

- **Go** 1.27+
- **Node.js** 20.9+ and **npm**
- **PostgreSQL** 17 (the bundled Compose image)
- **Docker Compose** and **PowerShell 7** for the local bootstrap scripts

### Local Setup

```bash
# Clone the repository
git clone https://github.com/alex-bogatiuk/metapus.git
cd metapus

# Infrastructure, tenant, migrations, seed and local signing configuration
pwsh ./scripts/dev/crypto-setup.ps1

# Backend (keep running in a separate terminal)
pwsh ./scripts/dev/crypto-run-server.ps1

# Frontend (another terminal)
cd frontend
npm ci
npm run dev
```

---

## 📋 Contribution Workflow

1. **Fork** the repository and create a feature branch:
   ```bash
   git checkout -b feature/my-feature
   ```

2. **Make your changes**, following the coding guidelines below.

3. **Run checks** before committing:
   ```bash
   # Backend
   go vet ./...
   go test ./...

   # Frontend
   cd frontend
   npm run typecheck
   npm run lint
   ```

4. **Commit** with a clear, descriptive message:
   ```
   feat(catalogs): add contractor catalog
   fix(documents): correct VAT calculation rounding
   ```

5. **Open a Pull Request** against the `main` branch.

---

## 🏗️ Coding Guidelines

### Backend (Go)

- Dependencies point inward: `infrastructure` / `application` → `domain` → `core`.
  Composition roots in `cmd/` wire the implementations.
- Use **Generic base types** (`BaseCatalogRepo[T]`, `CatalogService[T]`, `BaseHandler[T]`)
  for standard CRUD — do not copy-paste boilerplate.
- Keep business logic in the **domain layer** — no `http` or `pgx` imports in domain models.
- All new entities must have `Validate(ctx)` method.
- Use `apperror.AppError` for error handling.

### Frontend (TypeScript / React)

- **No `any`** in new code.
- Use **Factory Pattern** for API clients (`createCatalogApi`, `createDocumentApi`).
- Use **Generic Page Components** (`CatalogListPage`, `ListContent`) for list views.
- Use **Custom Hooks** (`useCatalogForm`, `useDocumentListPage`) for orchestration.
- Money/amounts must respect `decimalPlaces` — never hardcode divisors like `100`.

### General

- Write meaningful commit messages (see format above).
- Add/update tests for new functionality.
- Keep PRs focused — one feature or fix per PR.

---

## 🐛 Bug Reports

Please open an issue with:

- A clear title and description.
- Steps to reproduce.
- Expected vs. actual behavior.
- Environment details (OS, Go version, Node version, browser).

---

## 💡 Feature Requests

Open an issue with the `enhancement` label. Describe:

- The problem you're trying to solve.
- Your proposed solution.
- Any alternatives you've considered.

---

## 📜 License

By contributing to Metapus, you agree that your contributions will be licensed
under the [AGPL-3.0 License](./LICENSE), subject to the terms of the
[CLA](./CLA.md).

---

Thank you for helping make Metapus better! 🚀
