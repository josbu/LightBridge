const style = `
.lb-openai-provider { font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; padding: 20px; color: #111827; }
.lb-openai-provider h2 { margin: 0 0 8px; font-size: 18px; font-weight: 650; }
.lb-openai-provider p { margin: 0 0 16px; color: #4b5563; font-size: 13px; line-height: 1.5; }
.lb-openai-provider label { display: block; margin: 12px 0 6px; color: #374151; font-size: 12px; font-weight: 600; }
.lb-openai-provider input, .lb-openai-provider textarea { box-sizing: border-box; width: 100%; border: 1px solid #d1d5db; border-radius: 6px; padding: 9px 10px; font-size: 13px; }
.lb-openai-provider textarea { min-height: 86px; resize: vertical; }
.lb-openai-provider button { border: 1px solid #111827; border-radius: 6px; background: #111827; color: #fff; padding: 8px 10px; font-size: 13px; cursor: pointer; }
.lb-openai-provider a { color: #0f766e; font-size: 13px; word-break: break-all; }
`;

function ensureStyle() {
  if (document.getElementById("lb-openai-provider-style")) return;
  const node = document.createElement("style");
  node.id = "lb-openai-provider-style";
  node.textContent = style;
  document.head.appendChild(node);
}

const OpenAIProviderSettings = {
  name: "OpenAIProviderSettings",
  mounted() {
    ensureStyle();
  },
  template: `
    <section class="lb-openai-provider">
      <h2>OpenAI Provider</h2>
      <p>OpenAI module provider is installed. Accounts created for this provider route through the module sidecar.</p>
    </section>
  `,
};

const OpenAIAccountForm = {
  name: "OpenAIAccountForm",
  props: {
    modelValue: { type: Object, default: () => ({}) },
  },
  emits: ["update:modelValue"],
  mounted() {
    ensureStyle();
  },
  data() {
    return {
      authorizeUrl: "",
      redirectUri: "http://localhost:1455/auth/callback",
    };
  },
  methods: {
    updateSecret(key, value) {
      const current = this.modelValue || {};
      this.$emit("update:modelValue", {
        ...current,
        provider_id: "openai",
        module_id: "openai",
        credentials: {
          ...(current.credentials || {}),
          [key]: value,
        },
      });
    },
    updateExtra(key, value) {
      const current = this.modelValue || {};
      this.$emit("update:modelValue", {
        ...current,
        provider_id: "openai",
        module_id: "openai",
        extra: {
          ...(current.extra || {}),
          [key]: value,
        },
      });
    },
    async generateOAuthUrl() {
      const verifier = await generateCodeVerifier();
      const challenge = await generateCodeChallenge(verifier);
      const state = randomHex(32);
      const params = new URLSearchParams({
        response_type: "code",
        client_id: "app_EMoamEEZ73f0CkXaXp7hrann",
        redirect_uri: this.redirectUri,
        scope: "openid profile email offline_access",
        state,
        code_challenge: challenge,
        code_challenge_method: "S256",
        id_token_add_organizations: "true",
        codex_cli_simplified_flow: "true",
      });
      this.authorizeUrl = `https://auth.openai.com/oauth/authorize?${params.toString()}`;
      this.updateSecret("code_verifier", verifier);
      this.updateExtra("redirect_uri", this.redirectUri);
      this.updateExtra("oauth_state", state);
      this.updateExtra("type", "oauth");
    },
  },
  template: `
    <section class="lb-openai-provider">
      <h2>OpenAI Account</h2>
      <p>Use an OpenAI API key, paste migrated OAuth tokens, or generate an OAuth authorization URL and paste the callback code.</p>
      <label>API Key</label>
      <input autocomplete="off" spellcheck="false" placeholder="sk-..." @input="updateSecret('api_key', $event.target.value)" />
      <label>OAuth Redirect URI</label>
      <input autocomplete="off" spellcheck="false" :value="redirectUri" @input="redirectUri = $event.target.value; updateExtra('redirect_uri', $event.target.value)" />
      <button type="button" @click="generateOAuthUrl">Generate OAuth URL</button>
      <p v-if="authorizeUrl"><a :href="authorizeUrl" target="_blank" rel="noreferrer">{{ authorizeUrl }}</a></p>
      <label>OAuth Callback Code</label>
      <textarea autocomplete="off" spellcheck="false" @input="updateSecret('authorization_code', $event.target.value)"></textarea>
      <label>Access Token</label>
      <textarea autocomplete="off" spellcheck="false" @input="updateSecret('access_token', $event.target.value)"></textarea>
      <label>Refresh Token</label>
      <textarea autocomplete="off" spellcheck="false" @input="updateSecret('refresh_token', $event.target.value)"></textarea>
      <label>Base URL</label>
      <input autocomplete="off" spellcheck="false" placeholder="https://api.openai.com" @input="updateExtra('base_url', $event.target.value)" />
    </section>
  `,
};

async function generateCodeVerifier() {
  const bytes = new Uint8Array(64);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function generateCodeChallenge(verifier) {
  const data = new TextEncoder().encode(verifier);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return base64Url(new Uint8Array(digest));
}

function randomHex(size) {
  const bytes = new Uint8Array(size);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function base64Url(bytes) {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

export { OpenAIProviderSettings, OpenAIAccountForm };
