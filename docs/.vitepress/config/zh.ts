import type { DefaultTheme, LocaleSpecificConfig } from "vitepress";

export const zhConfig: LocaleSpecificConfig<DefaultTheme.Config> = {
  themeConfig: {
    nav: [
      { text: "首页", link: "/" },
      { text: "源码", link: "https://github.com/fengmuxi/frp-panel" },
    ],

    sidebar: [
      {
        text: "快速开始",
        collapsed: false,
        link: "/quick-start",
        items: [
          { text: "Master 部署", link: "/deploy-master" },
          { text: "Server 部署", link: "/deploy-server" },
          { text: "Client 部署", link: "/deploy-client" },
        ],
      },
      {
        text: "进阶使用",
        collapsed: false,
        link: "/wireguard",
        items: [
          { text: "WireGuard 多跳智能组网", link: "/wireguard" },
          { text: "函数功能（边缘 Worker）", link: "/worker" },
          { text: "邮件通知", link: "/mail-notification" },
        ],
      },
      {
        text: "配置说明",
        collapsed: false,
        link: "/all-configs",
      },
      {
        text: "CI/CD 与发布",
        collapsed: false,
        link: "/ci-cd",
      },
      {
        text: "贡献指南",
        collapsed: false,
        link: "/contribute",
      },
      {
        text: "常见问题",
        collapsed: false,
        link: "/faq",
      },
      {
        text: "截图展示",
        collapsed: false,
        link: "/screenshots",
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/fengmuxi/frp-panel" },
    ],
  },
};
