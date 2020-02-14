/*
Copyright 2020 caicloud authors. All rights reserved.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/caicloud/clientset/pkg/apis/resource/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// StorageTypeLister helps list StorageTypes.
type StorageTypeLister interface {
	// List lists all StorageTypes in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.StorageType, err error)
	// Get retrieves the StorageType from the index for a given name.
	Get(name string) (*v1alpha1.StorageType, error)
	StorageTypeListerExpansion
}

// storageTypeLister implements the StorageTypeLister interface.
type storageTypeLister struct {
	indexer cache.Indexer
}

// NewStorageTypeLister returns a new StorageTypeLister.
func NewStorageTypeLister(indexer cache.Indexer) StorageTypeLister {
	return &storageTypeLister{indexer: indexer}
}

// List lists all StorageTypes in the indexer.
func (s *storageTypeLister) List(selector labels.Selector) (ret []*v1alpha1.StorageType, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.StorageType))
	})
	return ret, err
}

// Get retrieves the StorageType from the index for a given name.
func (s *storageTypeLister) Get(name string) (*v1alpha1.StorageType, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("storagetype"), name)
	}
	return obj.(*v1alpha1.StorageType), nil
}
