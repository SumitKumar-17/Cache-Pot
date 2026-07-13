import { defineConfig } from "vitepress";

export default defineConfig({
  title: "Cache-Pot",
  description: "The memory engine for AI agents",
  cleanUrls: true,

  // _generated-table.md is a partial pulled into commands/index.md via a
  // markdown @include, not a standalone page — exclude it from routing so
  // it doesn't also get built as its own orphaned, unlinked route.
  srcExclude: ["**/_generated-table.md"],

  themeConfig: {
    nav: [
      { text: "Home", link: "/" },
      { text: "Getting Started", link: "/getting-started/installation" },
      { text: "Commands", link: "/commands/" },
      { text: "Architecture", link: "/architecture/overview" },
      { text: "Roadmap", link: "/roadmap/" },
    ],

    sidebar: [
      {
        text: "Getting Started",
        items: [
          { text: "Installation", link: "/getting-started/installation" },
          { text: "Quickstart", link: "/getting-started/quickstart" },
          { text: "Configuration", link: "/getting-started/configuration" },
          { text: "MCP Server", link: "/getting-started/mcp-server" },
          { text: "Observability", link: "/getting-started/observability" },
        ],
      },
      {
        text: "Commands",
        items: [
          { text: "All Commands", link: "/commands/" },
          { text: "Connection", link: "/commands/connection" },
          { text: "Generic (Keys/TTL)", link: "/commands/generic" },
          { text: "Strings", link: "/commands/strings" },
          { text: "Hashes", link: "/commands/hashes" },
          { text: "Lists", link: "/commands/lists" },
          { text: "Sets", link: "/commands/sets" },
          { text: "Sorted Sets", link: "/commands/sorted-sets" },
          { text: "Pub/Sub & Transactions", link: "/commands/pubsub-and-transactions" },
          { text: "Semantic Cache", link: "/commands/semantic-cache" },
          { text: "Tool Cache", link: "/commands/tool-cache" },
          { text: "Vector Search", link: "/commands/vector" },
          { text: "Agent Memory", link: "/commands/memory" },
          { text: "Knowledge Graph (Planned)", link: "/commands/graph" },
          { text: "Versioning (Planned)", link: "/commands/versioning" },
        ],
      },
      {
        text: "Architecture",
        items: [
          { text: "Overview", link: "/architecture/overview" },
          { text: "Storage Engine", link: "/architecture/storage-engine" },
          { text: "Redis Compatibility", link: "/architecture/redis-compatibility" },
        ],
      },
      {
        text: "Roadmap",
        items: [{ text: "7-Phase Roadmap", link: "/roadmap/" }],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/SumitKumar-17/cache-pot" },
    ],

    search: {
      provider: "local",
    },
  },
});
