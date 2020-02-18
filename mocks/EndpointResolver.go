// Code generated by mockery v1.0.0. DO NOT EDIT.

package mocks

import elbv2 "github.com/aws/aws-sdk-go/service/elbv2"
import mock "github.com/stretchr/testify/mock"
import v1beta1 "k8s.io/api/networking/v1beta1"

// EndpointResolver is an autogenerated mock type for the EndpointResolver type
type EndpointResolver struct {
	mock.Mock
}

// Resolve provides a mock function with given fields: _a0, _a1, _a2
func (_m *EndpointResolver) Resolve(_a0 *v1beta1.Ingress, _a1 *v1beta1.IngressBackend, _a2 string) ([]*elbv2.TargetDescription, error) {
	ret := _m.Called(_a0, _a1, _a2)

	var r0 []*elbv2.TargetDescription
	if rf, ok := ret.Get(0).(func(*v1beta1.Ingress, *v1beta1.IngressBackend, string) []*elbv2.TargetDescription); ok {
		r0 = rf(_a0, _a1, _a2)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*elbv2.TargetDescription)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*v1beta1.Ingress, *v1beta1.IngressBackend, string) error); ok {
		r1 = rf(_a0, _a1, _a2)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
