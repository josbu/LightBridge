import { computed, onUnmounted, reactive, watch } from "vue";
import {
  affiliatesAPI,
  type AffiliateAdminEntry,
  type SimpleUser as AffiliateSimpleUser,
} from "@/api/admin/affiliates";
import { extractApiErrorMessage } from "@/utils/apiError";

type Translate = (key: string, params?: Record<string, unknown>) => string;

interface AffiliateState {
  loading: boolean;
  entries: AffiliateAdminEntry[];
  total: number;
  page: number;
  pageSize: number;
  search: string;
  selected: number[];
  searchTimer: number | null;
}

interface AffiliateModalState {
  open: boolean;
  mode: "add" | "edit";
  saving: boolean;
  userQuery: string;
  userResults: AffiliateSimpleUser[];
  selectedUser: AffiliateSimpleUser | null;
  editingEntry: AffiliateAdminEntry | null;
  code: string;
  rate: string | number;
  searchTimer: number | null;
}

interface UseAffiliateUserSettingsOptions {
  isEnabled(): boolean;
  t: Translate;
  showError(message: string): void;
  showSuccess(message: string): void;
}

export function useAffiliateUserSettings({
  isEnabled,
  t,
  showError,
  showSuccess,
}: UseAffiliateUserSettingsOptions) {
  const affiliateState = reactive<AffiliateState>({
    loading: false,
    entries: [],
    total: 0,
    page: 1,
    pageSize: 20,
    search: "",
    selected: [],
    searchTimer: null,
  });

  const affiliateModal = reactive<AffiliateModalState>({
    open: false,
    mode: "add",
    saving: false,
    userQuery: "",
    userResults: [],
    selectedUser: null,
    editingEntry: null,
    code: "",
    rate: "",
    searchTimer: null,
  });

  const affiliateBatchModal = reactive({
    open: false,
    saving: false,
    rate: "" as string | number,
  });

  const affiliateConfirmDialog = reactive<{
    show: boolean;
    title: string;
    message: string;
    confirmText: string;
    pending: (() => Promise<unknown>) | null;
  }>({
    show: false,
    title: "",
    message: "",
    confirmText: "",
    pending: null,
  });

  function clearTimer(slot: { searchTimer: number | null }): void {
    if (slot.searchTimer == null) return;
    window.clearTimeout(slot.searchTimer);
    slot.searchTimer = null;
  }

  function debounce(
    slot: { searchTimer: number | null },
    delayMs: number,
    run: () => void,
  ): void {
    clearTimer(slot);
    slot.searchTimer = window.setTimeout(run, delayMs);
  }

  function parseRebateRate(raw: unknown): number | null | undefined {
    const value = String(raw ?? "").trim();
    if (value === "") return null;
    const parsed = Number(value);
    if (Number.isNaN(parsed) || parsed < 0 || parsed > 100) {
      showError(t("admin.settings.features.affiliate.modal.errorBadRate"));
      return undefined;
    }
    return parsed;
  }

  async function loadAffiliateUsers(): Promise<void> {
    affiliateState.loading = true;
    try {
      const response = await affiliatesAPI.listUsers({
        page: affiliateState.page,
        page_size: affiliateState.pageSize,
        search: affiliateState.search,
      });
      affiliateState.entries = response.items ?? [];
      affiliateState.total = response.total ?? 0;
      const visibleIDs = new Set(
        affiliateState.entries.map((entry) => entry.user_id),
      );
      affiliateState.selected = affiliateState.selected.filter((id) =>
        visibleIDs.has(id),
      );
    } catch (error: unknown) {
      showError(extractApiErrorMessage(error, t("common.error")));
    } finally {
      affiliateState.loading = false;
    }
  }

  function openAffiliateConfirm(
    title: string,
    message: string,
    confirmText: string,
    run: () => Promise<unknown>,
  ): void {
    affiliateConfirmDialog.title = title;
    affiliateConfirmDialog.message = message;
    affiliateConfirmDialog.confirmText = confirmText;
    affiliateConfirmDialog.pending = run;
    affiliateConfirmDialog.show = true;
  }

  async function handleAffiliateConfirm(): Promise<void> {
    const run = affiliateConfirmDialog.pending;
    affiliateConfirmDialog.show = false;
    affiliateConfirmDialog.pending = null;
    if (!run) return;
    try {
      await run();
      showSuccess(t("common.saved"));
      await loadAffiliateUsers();
    } catch (error: unknown) {
      showError(extractApiErrorMessage(error, t("common.error")));
    }
  }

  function cancelAffiliateConfirm(): void {
    affiliateConfirmDialog.show = false;
    affiliateConfirmDialog.pending = null;
  }

  function onAffiliateSearchInput(): void {
    debounce(affiliateState, 300, () => {
      affiliateState.page = 1;
      void loadAffiliateUsers();
    });
  }

  function changeAffiliatePage(page: number): void {
    if (page < 1) return;
    affiliateState.page = page;
    void loadAffiliateUsers();
  }

  function toggleAffiliateSelectAll(event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    affiliateState.selected = checked
      ? affiliateState.entries.map((entry) => entry.user_id)
      : [];
  }

  function toggleAffiliateSelect(userID: number): void {
    const index = affiliateState.selected.indexOf(userID);
    if (index >= 0) {
      affiliateState.selected.splice(index, 1);
      return;
    }
    affiliateState.selected.push(userID);
  }

  function openAffiliateModal(entry: AffiliateAdminEntry | null): void {
    affiliateModal.open = true;
    affiliateModal.mode = entry ? "edit" : "add";
    affiliateModal.userQuery = "";
    affiliateModal.userResults = [];
    affiliateModal.selectedUser = null;
    affiliateModal.editingEntry = entry;
    affiliateModal.code = entry?.aff_code_custom ? entry.aff_code : "";
    affiliateModal.rate =
      entry?.aff_rebate_rate_percent != null
        ? String(entry.aff_rebate_rate_percent)
        : "";
  }

  function closeAffiliateModal(): void {
    affiliateModal.open = false;
    clearTimer(affiliateModal);
  }

  function onAffiliateUserSearchInput(): void {
    const query = affiliateModal.userQuery.trim();
    if (!query) {
      affiliateModal.userResults = [];
      clearTimer(affiliateModal);
      return;
    }
    debounce(affiliateModal, 300, () => {
      void (async () => {
        try {
          affiliateModal.userResults = await affiliatesAPI.lookupUsers(query);
        } catch (error: unknown) {
          showError(extractApiErrorMessage(error, t("common.error")));
        }
      })();
    });
  }

  function selectAffiliateUser(user: AffiliateSimpleUser): void {
    affiliateModal.selectedUser = user;
    affiliateModal.userQuery = "";
    affiliateModal.userResults = [];
  }

  function clearSelectedAffiliateUser(): void {
    affiliateModal.selectedUser = null;
  }

  const affiliateModalCanSubmit = computed(() => {
    if (affiliateModal.mode === "add") {
      if (!affiliateModal.selectedUser) return false;
    } else if (!affiliateModal.editingEntry) {
      return false;
    }
    const codeFilled = affiliateModal.code.trim() !== "";
    const rateFilled = String(affiliateModal.rate ?? "").trim() !== "";
    if (codeFilled || rateFilled) return true;
    return (
      affiliateModal.mode === "edit" &&
      affiliateModal.editingEntry?.aff_rebate_rate_percent != null
    );
  });

  async function submitAffiliateModal(): Promise<void> {
    if (!affiliateModalCanSubmit.value) {
      showError(t("admin.settings.features.affiliate.modal.errorEmpty"));
      return;
    }

    const userID =
      affiliateModal.mode === "add"
        ? affiliateModal.selectedUser!.id
        : affiliateModal.editingEntry!.user_id;
    const payload: Parameters<typeof affiliatesAPI.updateUserSettings>[1] = {};
    const code = affiliateModal.code.trim();
    if (code) payload.aff_code = code.toUpperCase();

    const rate = parseRebateRate(affiliateModal.rate);
    if (rate === undefined) return;
    if (rate === null) {
      if (
        affiliateModal.mode === "edit" &&
        affiliateModal.editingEntry?.aff_rebate_rate_percent != null
      ) {
        payload.clear_rebate_rate = true;
      }
    } else {
      payload.aff_rebate_rate_percent = rate;
    }

    affiliateModal.saving = true;
    try {
      await affiliatesAPI.updateUserSettings(userID, payload);
      showSuccess(t("common.saved"));
      closeAffiliateModal();
      affiliateState.page = 1;
      await loadAffiliateUsers();
    } catch (error: unknown) {
      showError(extractApiErrorMessage(error, t("common.error")));
    } finally {
      affiliateModal.saving = false;
    }
  }

  function askResetAffiliateUser(entry: AffiliateAdminEntry): void {
    openAffiliateConfirm(
      t("admin.settings.features.affiliate.customUsers.resetTitle"),
      t("admin.settings.features.affiliate.customUsers.resetMessage", {
        email: entry.email || `#${entry.user_id}`,
      }),
      t("common.delete"),
      () => affiliatesAPI.clearUserSettings(entry.user_id),
    );
  }

  function openAffiliateBatchModal(): void {
    if (affiliateState.selected.length === 0) return;
    affiliateBatchModal.open = true;
    affiliateBatchModal.rate = "";
  }

  async function submitAffiliateBatchModal(): Promise<void> {
    const rate = parseRebateRate(affiliateBatchModal.rate);
    if (rate === undefined) return;
    const userIDs = [...affiliateState.selected];
    const payload: Parameters<typeof affiliatesAPI.batchSetRate>[0] =
      rate === null
        ? { user_ids: userIDs, clear: true }
        : { user_ids: userIDs, aff_rebate_rate_percent: rate };

    affiliateBatchModal.saving = true;
    try {
      await affiliatesAPI.batchSetRate(payload);
      showSuccess(t("common.saved"));
      affiliateBatchModal.open = false;
      affiliateState.selected = [];
      await loadAffiliateUsers();
    } catch (error: unknown) {
      showError(extractApiErrorMessage(error, t("common.error")));
    } finally {
      affiliateBatchModal.saving = false;
    }
  }

  watch(isEnabled, (enabled, previous) => {
    if (enabled && !previous) {
      void loadAffiliateUsers();
    }
  });

  onUnmounted(() => {
    clearTimer(affiliateState);
    clearTimer(affiliateModal);
  });

  return {
    affiliateBatchModal,
    affiliateConfirmDialog,
    affiliateModal,
    affiliateModalCanSubmit,
    affiliateState,
    askResetAffiliateUser,
    cancelAffiliateConfirm,
    changeAffiliatePage,
    clearSelectedAffiliateUser,
    closeAffiliateModal,
    handleAffiliateConfirm,
    loadAffiliateUsers,
    onAffiliateSearchInput,
    onAffiliateUserSearchInput,
    openAffiliateBatchModal,
    openAffiliateModal,
    selectAffiliateUser,
    submitAffiliateBatchModal,
    submitAffiliateModal,
    toggleAffiliateSelect,
    toggleAffiliateSelectAll,
  };
}
