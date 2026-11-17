export const ROUTES = {
	home: '/',
	workers: '/workers',
	workerEdit: '/worker-edit',
	wg: {
		networks: '/wg/networks',
		networkDetail: '/wg/network-detail',
		wireguards: '/wg/wireguards',
		wireguardDetail: '/wg/wireguard-detail',
		endpoints: '/wg/endpoints',
		endpointDetail: '/wg/endpoint-detail',
		links: '/wg/links',
	},
} as const

/**
 * 二级（详情/编辑）页面 → 所属一级页面的映射
 * Header 根据此配置渲染"返回"按钮，侧边栏根据它高亮父级菜单
 */
export const PAGE_PARENTS: Record<string, string> = {
	'/clientedit': '/clients',
	'/serveredit': '/servers',
	'/worker-edit': ROUTES.workers,
	'/wg/network-detail': ROUTES.wg.networks,
	'/wg/wireguard-detail': ROUTES.wg.wireguards,
	'/wg/endpoint-detail': ROUTES.wg.endpoints,
	'/canvas': '/console',
	'/user-info': ROUTES.home,
	'/platform-settings': ROUTES.home,
	'/mail-settings': ROUTES.home,
}

/** 一级页面路径 → i18n 标题键（用于返回按钮上的文案） */
export const NAV_TITLE_KEYS: Record<string, string> = {
	'/': 'nav.home',
	'/clients': 'nav.clients',
	'/servers': 'nav.servers',
	'/proxies': 'nav.editTunnel',
	'/clientstats': 'nav.trafficStats',
	'/streamlog': 'nav.realTimeLog',
	'/console': 'nav.console',
	'/workers': 'nav.workers',
	'/wg/networks': 'wg.nav.networks',
	'/wg/wireguards': 'wg.nav.wireguards',
	'/wg/endpoints': 'wg.nav.endpoints',
	'/wg/links': 'wg.nav.links',
}


