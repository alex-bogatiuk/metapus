package domain

// WrapRepository replaces the service's repository with wrap(repository):
// an add-on's extension of the catalog is read and written with its items
// this way, in the transaction of every write of the service.
func (s *CatalogService[T]) WrapRepository(wrap func(CatalogRepository[T]) CatalogRepository[T]) {
	s.repo = wrap(s.repo)
}

// EntityName is the catalog's entity key.
func (s *CatalogService[T]) EntityName() string { return s.entityName }
