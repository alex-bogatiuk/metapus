// document_contract.go — Documentation for document extension patterns.
//
// The concrete DocumentRegistration interface lives in v1/document_factory.go
// because it depends on DocumentDeps (which references concrete infrastructure types).
//
// Client extensions implement v1.DocumentRegistration directly.
// Optional interfaces from this package (Presentable, Inspectable, Labeled)
// apply to both catalog and document registrations.
package platform
