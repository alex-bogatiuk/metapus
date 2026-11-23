// Package posting plugs a configuration's accumulation registers into
// document posting: a Module adds a visitor that collects a document's
// movements and a recorder that writes them.
package posting

import (
	core "github.com/alex-bogatiuk/metapus/internal/domain/posting"
)

type (
	// Module installs registers into the posting engine
	// (configuration.Server.PostingModules).
	Module = core.Module
	// Engine posts documents.
	Engine = core.Engine
	// Postable is a document the engine posts.
	Postable = core.Postable
	// MovementSet holds the movements a posting collected; a register keeps
	// its own under its name (SetExtension, GetExtension).
	MovementSet = core.MovementSet
	// RegisterVisitor collects a register's movements from a document.
	RegisterVisitor = core.RegisterVisitor
	// RegisterRecorder writes and reverses a register's movements.
	RegisterRecorder = core.RegisterRecorder
	// PostingValidator checks movements before they are written (for
	// example a balance that may not go negative).
	PostingValidator = core.PostingValidator
)

// NewEngine returns an engine without document locking, for tests.
func NewEngine() *Engine { return core.NewEngine(nil) }

// DocumentLocker locks a document while the engine posts it.
type DocumentLocker = core.DocumentLocker

// NewLockingEngine returns an engine that locks each document with
// docLocker while it posts it and writes movements with recorders.
func NewLockingEngine(docLocker DocumentLocker, recorders ...RegisterRecorder) *Engine {
	return core.NewEngine(docLocker, recorders...)
}

type (
	// LineCounter is an optional interface for Postable documents
	// that can report their line count for pre-allocation hints.
	LineCounter = core.LineCounter
	// PostingResourceLocker runs before evidence validators, balances and old
	// movement reversals. It locks a complete resource set, without writing money.
	PostingResourceLocker = core.PostingResourceLocker
	// UnpostGuard is an optional domain-level invariant for documents whose
	// movements may only be corrected by a new compensating document. The engine
	// checks it before opening a transaction or reversing any register movements.
	UnpostGuard = core.UnpostGuard
)
