package f5lb

import (
	"fmt"

	"github.com/caicloud/loadbalancer-provider/core/provider"
)

/*
1. get active view
> https://{XXX.XX.XXX.XXX}/wapi/{v2.3.1}/view?_return_fields=name&_return_fields=disable
<
[ { u'_ref': u'view/ZG5zLnZpZXckLl9kZWZhdWx0:default/true',
    u'disable': True,
    u'name': u'default'},
  { u'_ref': u'view/ZG5zLnZpZXckLjc:DR_view/false',
    u'disable': False,
    u'name': u'DR_view'},
  { u'_ref': u'view/ZG5zLnZpZXckLjEx:Default_view/false',
    u'disable': True,
    u'name': u'Default_view'},
  { u'_ref': u'view/ZG5zLnZpZXckLjEz:view_01/false',
    u'disable': True,
    u'name': u'view_01'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE0:view_02/false',
    u'disable': True,
    u'name': u'view_02'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE1:view_03/false',
    u'disable': True,
    u'name': u'view_03'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE2:view_04/false',
    u'disable': True,
    u'name': u'view_04'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE3:view_05/false',
    u'disable': True,
    u'name': u'view_05'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE4:view_06/false',
    u'disable': True,
    u'name': u'view_06'},
  { u'_ref': u'view/ZG5zLnZpZXckLjE5:view_07/false',
    u'disable': True,
    u'name': u'view_07'},
  { u'_ref': u'view/ZG5zLnZpZXckLjIw:view_08/false',
    u'disable': True,
    u'name': u'view_08'}]

2. check zone
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/zone_auth?view={active_view}&fqdn={zone}
<
'''
[ { u'_ref': u'zone_auth/ZG5zLnpvbmUkLjcuY29tLnRlc3QtamFja3k:test-jacky.com/DR_view',
    u'fqdn': u'test-jacky.com',
    u'view': u'DR_view'}]
'''

3. create
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a  # or record:cname
> A: {"name":"test1.test-jacky.com", "ipv4addr":"99.99.99.90", "comment": "测试1", "view": "aview"}
> CNAME: {"name":"test3.test-jacky.com", "canonical":"test2.test-jacky.com", "comment": "测试2", "view": "aview"}

4. get
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a?view=aview&name=test1.test-jacky.com
< [ { u'_ref': u'record:a/ZG5zLmJpbmRfYSQuNy5jb20udGVzdC1qYWNreSx0ZXN0NSw3Ny43Ny43Ny43MA:test5.test-jacky.com/DR_view',
    u'ipv4addr': u'77.77.77.70',
    u'name': u'test5.test-jacky.com',
    u'view': u'DR_view'}]

4. update # url is from '_ref'
> https://XXX.XX.XXX.XXX/wapi/v2.3.1/record:a/ZG5zLmJpbmRfYSQuNy5jb20udGVzdC1qYWNreSx0ZXN0NSw3Ny43Ny43Ny43MA:test5.test-jacky.com/DR_view
> {"ipv4addr":"99.99.99.90", "comment": "测试1"}

*/

func newInfobloxClient(d provider.Device, lbnamespace, lbname string) (*f5DNSClient, error) {
	return nil, fmt.Errorf("NoImplement Infoblox dns")
}
