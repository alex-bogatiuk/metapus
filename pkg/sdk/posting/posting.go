// Package posting plugs a configuration's accumulation registers into
// document posting: a Module adds a visitor that collects a document's
// movements and a recorder that writes them.
package posting

import core "metapus/internal/domain/posting"

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
