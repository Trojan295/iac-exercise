package pkg

import "errors"

var ErrELBNotFound = errors.New("cannot find Elastic LoadBalancer")

var ErrInstancesNotInService = errors.New("new instances are not InService state")
