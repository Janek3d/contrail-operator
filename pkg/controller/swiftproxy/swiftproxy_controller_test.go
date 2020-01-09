package swiftproxy_test

import (
	"context"
	"testing"

	"github.com/Juniper/contrail-operator/pkg/controller/swiftproxy"
	"github.com/Juniper/contrail-operator/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	contrail "github.com/Juniper/contrail-operator/pkg/apis/contrail/v1alpha1"
)

func TestSwiftProxyController(t *testing.T) {
	scheme, err := contrail.SchemeBuilder.Build()
	require.NoError(t, err)
	require.NoError(t, core.SchemeBuilder.AddToScheme(scheme))
	require.NoError(t, apps.SchemeBuilder.AddToScheme(scheme))

	falseVal := false
	tests := []struct {
		name               string
		initObjs           []runtime.Object
		expectedDeployment *apps.Deployment
		expectedStatus     contrail.SwiftProxyStatus
		expectedConfigs    []*core.ConfigMap
		expectedKeystone   *contrail.Keystone
	}{
		{
			name: "creates a new deployment",
			// given
			initObjs: []runtime.Object{
				newSwiftProxy(contrail.SwiftProxyStatus{}),
				newKeystone(contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"}, nil),
			},

			// then
			expectedDeployment: newExpectedDeployment(apps.DeploymentStatus{}),
			expectedKeystone: newKeystone(
				contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"},
				[]meta.OwnerReference{{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &falseVal, &falseVal}},
			),
			expectedConfigs: []*core.ConfigMap{
				newExpectedSwiftProxyConfigMap(),
				newExpectedSwiftProxyInitConfigMap(),
			},
		},
		{
			name: "is idempotent",
			// given
			initObjs: []runtime.Object{
				newSwiftProxy(contrail.SwiftProxyStatus{}),
				newKeystone(
					contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"},
					[]meta.OwnerReference{{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &falseVal, &falseVal}},
				),
				newExpectedDeployment(apps.DeploymentStatus{}),
				newExpectedSwiftProxyConfigMap(),
				newExpectedSwiftProxyInitConfigMap(),
			},

			// then
			expectedDeployment: newExpectedDeployment(apps.DeploymentStatus{}),
			expectedKeystone: newKeystone(
				contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"},
				[]meta.OwnerReference{{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &falseVal, &falseVal}},
			),
			expectedConfigs: []*core.ConfigMap{
				newExpectedSwiftProxyConfigMap(),
				newExpectedSwiftProxyInitConfigMap(),
			},
		},
		{
			name: "updates status to active",
			// given
			initObjs: []runtime.Object{
				newSwiftProxy(contrail.SwiftProxyStatus{}),
				newKeystone(contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"}, nil),
				newExpectedDeployment(apps.DeploymentStatus{
					ReadyReplicas: 1,
				}),
			},

			// then
			expectedDeployment: newExpectedDeployment(apps.DeploymentStatus{ReadyReplicas: 1}),
			expectedKeystone: newKeystone(
				contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"},
				[]meta.OwnerReference{{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &falseVal, &falseVal}},
			),
			expectedStatus: contrail.SwiftProxyStatus{
				Active: true,
			},
		},
		{
			name: "updates status to not active",
			// given
			initObjs: []runtime.Object{
				newSwiftProxy(contrail.SwiftProxyStatus{Active: true}),
				newKeystone(contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"}, nil),
				newExpectedDeployment(apps.DeploymentStatus{}),
			},

			// then
			expectedDeployment: newExpectedDeployment(apps.DeploymentStatus{}),
			expectedKeystone: newKeystone(
				contrail.KeystoneStatus{Active: true, Node: "10.0.2.15:5555"},
				[]meta.OwnerReference{{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &falseVal, &falseVal}},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given state
			cl := fake.NewFakeClientWithScheme(scheme, tt.initObjs...)
			r := swiftproxy.NewReconciler(cl, scheme, k8s.New(cl, scheme))
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "swiftproxy",
					Namespace: "default",
				},
			}
			// when swift proxy is reconciled
			res, err := r.Reconcile(req)
			assert.NoError(t, err)
			assert.False(t, res.Requeue)

			// then expected Deployment is present
			dep := &apps.Deployment{}
			exDep := tt.expectedDeployment
			err = cl.Get(context.Background(), types.NamespacedName{
				Name:      exDep.Name,
				Namespace: exDep.Namespace,
			}, dep)

			assert.NoError(t, err)
			assert.Equal(t, exDep, dep)

			// then expected SwiftProxy status is set
			sp := &contrail.SwiftProxy{}
			err = cl.Get(context.Background(), req.NamespacedName, sp)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, sp.Status)

			for _, expConfig := range tt.expectedConfigs {
				configMap := &core.ConfigMap{}
				err = cl.Get(context.Background(), types.NamespacedName{
					Name:      expConfig.Name,
					Namespace: expConfig.Namespace,
				}, configMap)

				assert.NoError(t, err)
				assert.Equal(t, expConfig, configMap)
			}

			// then expected Keystone is updated
			k := &contrail.Keystone{}
			err = cl.Get(context.Background(), types.NamespacedName{
				Name:      k.Name,
				Namespace: k.Namespace,
			}, k)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedKeystone, k)
		})
	}
}

func newSwiftProxy(status contrail.SwiftProxyStatus) *contrail.SwiftProxy {

	return &contrail.SwiftProxy{
		ObjectMeta: meta.ObjectMeta{
			Name:      "swiftproxy",
			Namespace: "default",
		},
		Spec: contrail.SwiftProxySpec{
			ServiceConfiguration: contrail.SwiftProxyConfiguration{
				ListenPort:            5070,
				KeystoneInstance:      "keystone",
				KeystoneAdminPassword: "c0ntrail123",
				SwiftPassword:         "swiftpass",
			},
		},
		Status: status,
	}
}

func newExpectedDeployment(status apps.DeploymentStatus) *apps.Deployment {
	trueVal := true
	d := &apps.Deployment{
		ObjectMeta: meta.ObjectMeta{
			Name:      "swiftproxy-deployment",
			Namespace: "default",
			OwnerReferences: []meta.OwnerReference{
				{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &trueVal, &trueVal},
			},
			Labels: map[string]string{"SwiftProxy": "swiftproxy"},
		},
		Spec: apps.DeploymentSpec{
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Labels: map[string]string{"SwiftProxy": "swiftproxy"},
				},
				Spec: core.PodSpec{
					InitContainers: []core.Container{
						{
							Name:            "init",
							Image:           "localhost:5000/centos-binary-kolla-toolbox:master",
							ImagePullPolicy: core.PullAlways,
							VolumeMounts: []core.VolumeMount{
								core.VolumeMount{Name: "init-config-volume", MountPath: "/var/lib/ansible/register", ReadOnly: true},
							},
							Command: []string{"ansible-playbook"},
							Args:    []string{"/var/lib/ansible/register/register.yaml", "-e", "@/var/lib/ansible/register/config.yaml"},
						},
					},
					Containers: []core.Container{{
						Name:  "api",
						Image: "localhost:5000/centos-binary-swift-proxy-server:master",
						VolumeMounts: []core.VolumeMount{
							core.VolumeMount{Name: "config-volume", MountPath: "/var/lib/kolla/config_files/", ReadOnly: true},
						},
						Env: []core.EnvVar{{
							Name:  "KOLLA_SERVICE_NAME",
							Value: "swift-proxy-server",
						}, {
							Name:  "KOLLA_CONFIG_STRATEGY",
							Value: "COPY_ALWAYS",
						}},
					}},
					HostNetwork: true,
					Tolerations: []core.Toleration{
						{
							Operator: core.TolerationOpExists,
							Effect:   core.TaintEffectNoSchedule,
						},
						{
							Operator: core.TolerationOpExists,
							Effect:   core.TaintEffectNoExecute,
						},
					},
					Volumes: []core.Volume{
						{
							Name: "config-volume",
							VolumeSource: core.VolumeSource{
								ConfigMap: &core.ConfigMapVolumeSource{
									LocalObjectReference: core.LocalObjectReference{
										Name: "swiftproxy-swiftproxy-config",
									},
								},
							},
						},
						{
							Name: "init-config-volume",
							VolumeSource: core.VolumeSource{
								ConfigMap: &core.ConfigMapVolumeSource{
									LocalObjectReference: core.LocalObjectReference{
										Name: "swiftproxy-swiftproxy-init-config",
									},
								},
							},
						},
					},
				},
			},

			Selector: &meta.LabelSelector{
				MatchLabels: map[string]string{"SwiftProxy": "swiftproxy"},
			},
		},
		Status: status,
	}

	return d
}

func newKeystone(status contrail.KeystoneStatus, ownersReferences []meta.OwnerReference) *contrail.Keystone {
	return &contrail.Keystone{
		ObjectMeta: meta.ObjectMeta{
			Name:            "keystone",
			Namespace:       "default",
			OwnerReferences: ownersReferences,
		},
		Status: status,
	}
}

func newExpectedSwiftProxyConfigMap() *core.ConfigMap {
	trueVal := true
	return &core.ConfigMap{
		Data: map[string]string{
			"config.json":       swiftProxyServiceConfig,
			"swift.conf":        swiftConfig,
			"proxy-server.conf": proxyServerConfig,
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      "swiftproxy-swiftproxy-config",
			Namespace: "default",
			Labels:    map[string]string{"contrail_manager": "SwiftProxy", "SwiftProxy": "swiftproxy"},
			OwnerReferences: []meta.OwnerReference{
				{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &trueVal, &trueVal},
			},
		},
	}
}

func newExpectedSwiftProxyInitConfigMap() *core.ConfigMap {
	trueVal := true
	return &core.ConfigMap{
		Data: map[string]string{
			"register.yaml": registerPlaybook,
			"config.yaml":   registerConfig,
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      "swiftproxy-swiftproxy-init-config",
			Namespace: "default",
			Labels:    map[string]string{"contrail_manager": "SwiftProxy", "SwiftProxy": "swiftproxy"},
			OwnerReferences: []meta.OwnerReference{
				{"contrail.juniper.net/v1alpha1", "SwiftProxy", "swiftproxy", "", &trueVal, &trueVal},
			},
		},
	}
}

const swiftConfig = `
[swift-hash]
swift_hash_path_suffix = changeme
swift_hash_path_prefix = changeme
`

const swiftProxyServiceConfig = `{
    "command": "swift-proxy-server /etc/swift/proxy-server.conf --verbose",
    "config_files": [
		{
            "source": "/var/lib/kolla/config_files/swift.conf",
            "dest": "/etc/swift/swift.conf",
            "owner": "swift",
            "perm": "0640"
        },
        {
            "source": "/var/lib/kolla/config_files/proxy-server.conf",
            "dest": "/etc/swift/proxy-server.conf",
            "owner": "swift",
            "perm": "0640"
        }
    ]
}`

const proxyServerConfig = `
[DEFAULT]
bind_ip = 0.0.0.0
bind_port = 5070
log_udp_host =
log_udp_port = 5140
log_name = swift-proxy-server
log_facility = local0
log_level = INFO
workers = 2

[pipeline:main]
pipeline = catch_errors gatekeeper healthcheck cache container_sync bulk tempurl ratelimit authtoken keystoneauth container_quotas account_quotas slo dlo proxy-server

[app:proxy-server]
use = egg:swift#proxy
allow_account_management = true
account_autocreate = true

[filter:tempurl]
use = egg:swift#tempurl

[filter:cache]
use = egg:swift#memcache
memcache_servers = localhost:11211

[filter:catch_errors]
use = egg:swift#catch_errors

[filter:healthcheck]
use = egg:swift#healthcheck

[filter:proxy-logging]
use = egg:swift#proxy_logging

[filter:authtoken]
paste.filter_factory = keystonemiddleware.auth_token:filter_factory
auth_uri = http://10.0.2.15:5555
auth_url = http://10.0.2.15:5555
auth_type = password
project_domain_id = default
user_domain_id = default
project_name = service
username = swift
password = swiftpass
delay_auth_decision = False
memcache_security_strategy = None
memcached_servers = localhost:11211

[filter:keystoneauth]
use = egg:swift#keystoneauth
operator_roles = admin,_member_,ResellerAdmin

[filter:container_sync]
use = egg:swift#container_sync

[filter:bulk]
use = egg:swift#bulk

[filter:ratelimit]
use = egg:swift#ratelimit

[filter:gatekeeper]
use = egg:swift#gatekeeper

[filter:account_quotas]
use = egg:swift#account_quotas

[filter:container_quotas]
use = egg:swift#container_quotas

[filter:slo]
use = egg:swift#slo

[filter:dlo]
use = egg:swift#dlo

[filter:versioned_writes]
use = egg:swift#versioned_writes
allow_versioned_writes = True

`

const registerPlaybook = `
- hosts: localhost
  tasks:
    - name: create swift service
      os_keystone_service:
        name: "swift"
        service_type: "object-store"
        description: "object store service"
        interface: "admin"
        auth: "{{ openstack_auth }}"
    - name: create swift endpoints service
      os_keystone_endpoint:
        service: "swift"
        url: "{{ item.url }}"
        region: "RegionOne"
        endpoint_interface: "{{ item.interface }}"
        interface: "admin"
        auth: "{{ openstack_auth }}"
      with_items:
        - { url: "http://{{ swift_endpoint }}/v1", interface: "admin" }
        - { url: "http://{{ swift_endpoint }}/v1/AUTH_%(tenant_id)s", interface: "internal" }
        - { url: "http://{{ swift_endpoint }}/v1/AUTH_%(tenant_id)s", interface: "public" }
    - name: create service project
      os_project:
        name: "service"
        domain: "default"
        interface: "admin"
        auth: "{{ openstack_auth }}"
    - name: create swift user
      os_user:
        default_project: "service"
        name: "swift"
        password: "{{ swift_password }}"
        domain: "default"
        interface: "admin"
        auth: "{{ openstack_auth }}"
    - name: create admin role    
      os_keystone_role:
        name: "{{ item }}"
        interface: "admin"
        auth: "{{ openstack_auth }}"
      with_items:
        - admin
        - ResellerAdmin
    - name: grant user role 
      os_user_role:
        user: "swift"
        role: "admin"
        project: "service"
        domain: "default"
        interface: "admin"
        auth: "{{ openstack_auth }}"
`

var registerConfig = `
openstack_auth:
  auth_url: "http://10.0.2.15:5555/v3"
  username: "admin"
  password: "c0ntrail123"
  project_name: "admin"
  domain_id: "default"
  user_domain_id: "default"

swift_endpoint: "localhost:5070"
swift_password: "swiftpass"
`