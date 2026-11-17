import { defineConfig } from "vitepress";
import { zhConfig } from "./config/zh";
import { enConfig } from "./config/en";

// https://vitepress.dev/reference/site-config
export default defineConfig({
    base: "/frp-panel/",
    // 放行文档中指向仓库源码文件（.go/.ts 等）的引用：站点内天然不可达，
    // 这类链接仅作为源码定位说明保留
    ignoreDeadLinks: [/\.(go|ts|tsx)$/],
    locales: {
        root: {
            label: "简体中文",
            ...zhConfig,
        },
        en: {
            label: "English",
            ...enConfig,
        },
    },
    title: "Frp-Panel WIKI",
    description: "Wiki of frp-panel",
    themeConfig: {
        // https://vitepress.dev/reference/default-theme-config
    },
    vite: {
        server: {
            port: 5467,
            strictPort: true,
            host: "0.0.0.0",
            allowedHosts: true,
        },
    },
});
