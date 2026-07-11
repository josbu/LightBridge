import { computed, ref } from "vue";
import { adminAPI } from "@/api";
import { extractI18nErrorMessage } from "@/utils/apiError";
import type { ProviderInstance } from "@/types/payment";
import {
  findProviderEnablementConflict,
  type ProviderEnablementCandidate,
  type ProviderEnablementConflict,
} from "../model/paymentProviderRules";

type Translate = (key: string, params?: Record<string, unknown>) => string;

interface PaymentProviderDialogHandle {
  reset(defaultKey: string): void;
  loadProvider(provider: ProviderInstance): void;
}

interface PaymentProviderSettingsForm {
  payment_enabled_types: string[];
}

interface UsePaymentProviderSettingsOptions {
  form: PaymentProviderSettingsForm;
  t: Translate;
  showError(message: string): void;
  showSuccess(message: string): void;
  saveSettings(): Promise<void>;
}

export function usePaymentProviderSettings({
  form,
  t,
  showError,
  showSuccess,
  saveSettings,
}: UsePaymentProviderSettingsOptions) {
  const allPaymentTypes = computed(() => [
    { value: "easypay", label: t("payment.methods.easypay") },
    { value: "alipay", label: t("payment.methods.alipay") },
    { value: "wxpay", label: t("payment.methods.wxpay") },
    { value: "stripe", label: t("payment.methods.stripe") },
    { value: "airwallex", label: t("payment.methods.airwallex") },
  ]);

  const providersLoading = ref(false);
  const providerSaving = ref(false);
  const providers = ref<ProviderInstance[]>([]);
  const showProviderDialog = ref(false);
  const showDeleteProviderDialog = ref(false);
  const editingProvider = ref<ProviderInstance | null>(null);
  const deletingProviderId = ref<number | null>(null);
  const providerDialogRef = ref<PaymentProviderDialogHandle | null>(null);

  const providerKeyOptions = computed(() => [
    { value: "easypay", label: t("admin.settings.payment.providerEasypay") },
    { value: "alipay", label: t("admin.settings.payment.providerAlipay") },
    { value: "wxpay", label: t("admin.settings.payment.providerWxpay") },
    { value: "stripe", label: t("admin.settings.payment.providerStripe") },
    {
      value: "airwallex",
      label: t("admin.settings.payment.providerAirwallex"),
    },
  ]);

  const enabledProviderKeyOptions = computed(() => {
    const enabled = form.payment_enabled_types;
    return providerKeyOptions.value.filter((option) =>
      enabled.includes(option.value),
    );
  });

  const loadBalanceOptions = computed(() => [
    {
      value: "round-robin",
      label: t("admin.settings.payment.strategyRoundRobin"),
    },
    {
      value: "least-amount",
      label: t("admin.settings.payment.strategyLeastAmount"),
    },
  ]);

  const cancelRateLimitUnitOptions = computed(() => [
    {
      value: "minute",
      label: t("admin.settings.payment.cancelRateLimitUnitMinute"),
    },
    {
      value: "hour",
      label: t("admin.settings.payment.cancelRateLimitUnitHour"),
    },
    {
      value: "day",
      label: t("admin.settings.payment.cancelRateLimitUnitDay"),
    },
  ]);

  const cancelRateLimitModeOptions = computed(() => [
    {
      value: "rolling",
      label: t("admin.settings.payment.cancelRateLimitWindowModeRolling"),
    },
    {
      value: "fixed",
      label: t("admin.settings.payment.cancelRateLimitWindowModeFixed"),
    },
  ]);

  const hasAnyPaymentTypeEnabled = computed(
    () => form.payment_enabled_types.length > 0,
  );

  function isPaymentTypeEnabled(type: string): boolean {
    return form.payment_enabled_types.includes(type);
  }

  function showEnablementConflict(conflict: ProviderEnablementConflict): void {
    showError(
      t("admin.settings.payment.enableConflict", {
        method: t(`payment.methods.${conflict.method}`),
        provider: conflict.conflicting.name,
      }),
    );
  }

  function conflictFor(
    candidate: ProviderEnablementCandidate,
  ): ProviderEnablementConflict | null {
    return findProviderEnablementConflict(candidate, providers.value);
  }

  async function loadProviders(): Promise<void> {
    providersLoading.value = true;
    try {
      const response = await adminAPI.payment.getProviders();
      providers.value = response.data || [];
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
    } finally {
      providersLoading.value = false;
    }
  }

  async function disableProvidersByType(type: string): Promise<void> {
    const matching = providers.value.filter(
      (provider) => provider.provider_key === type && provider.enabled,
    );
    for (const provider of matching) {
      try {
        await adminAPI.payment.updateProvider(provider.id, { enabled: false });
        provider.enabled = false;
      } catch (error: unknown) {
        console.warn("[payment] disable provider failed", provider.id, error);
      }
    }
  }

  function togglePaymentType(type: string): void {
    if (form.payment_enabled_types.includes(type)) {
      form.payment_enabled_types = form.payment_enabled_types.filter(
        (enabledType) => enabledType !== type,
      );
      void disableProvidersByType(type);
      return;
    }
    form.payment_enabled_types = [...form.payment_enabled_types, type];
  }

  function openCreateProvider(): void {
    editingProvider.value = null;
    providerDialogRef.value?.reset(
      enabledProviderKeyOptions.value[0]?.value || "easypay",
    );
    showProviderDialog.value = true;
  }

  function openEditProvider(provider: ProviderInstance): void {
    editingProvider.value = provider;
    providerDialogRef.value?.loadProvider(provider);
    showProviderDialog.value = true;
  }

  async function handleSaveProvider(
    payload: Partial<ProviderInstance>,
  ): Promise<void> {
    providerSaving.value = true;
    try {
      const candidate: ProviderEnablementCandidate = {
        id: editingProvider.value?.id ?? 0,
        provider_key:
          payload.provider_key ?? editingProvider.value?.provider_key ?? "",
        supported_types:
          payload.supported_types ??
          editingProvider.value?.supported_types ??
          [],
        enabled: payload.enabled ?? editingProvider.value?.enabled ?? false,
        name: payload.name ?? editingProvider.value?.name ?? "",
      };
      const conflict = conflictFor(candidate);
      if (conflict) {
        showEnablementConflict(conflict);
        return;
      }

      if (editingProvider.value) {
        await adminAPI.payment.updateProvider(editingProvider.value.id, payload);
      } else {
        await adminAPI.payment.createProvider(payload);
      }
      showProviderDialog.value = false;
      await loadProviders();
      await saveSettings();
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
    } finally {
      providerSaving.value = false;
    }
  }

  async function handleToggleField(
    provider: ProviderInstance,
    field: "enabled" | "refund_enabled" | "allow_user_refund",
  ): Promise<void> {
    let newValue: boolean;
    if (field === "enabled") newValue = !provider.enabled;
    else if (field === "refund_enabled") newValue = !provider.refund_enabled;
    else newValue = !provider.allow_user_refund;

    if (field === "enabled" && newValue) {
      const conflict = conflictFor({
        id: provider.id,
        provider_key: provider.provider_key,
        supported_types: provider.supported_types,
        enabled: true,
        name: provider.name,
      });
      if (conflict) {
        showEnablementConflict(conflict);
        return;
      }
    }

    const payload: Record<string, boolean> = { [field]: newValue };
    if (field === "refund_enabled" && !newValue) {
      payload.allow_user_refund = false;
    }
    try {
      await adminAPI.payment.updateProvider(provider.id, payload);
      await loadProviders();
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
    }
  }

  async function handleToggleType(
    provider: ProviderInstance,
    type: string,
  ): Promise<void> {
    const updated = provider.supported_types.includes(type)
      ? provider.supported_types.filter((item) => item !== type)
      : [...provider.supported_types, type];
    const conflict = conflictFor({
      id: provider.id,
      provider_key: provider.provider_key,
      supported_types: updated,
      enabled: provider.enabled,
      name: provider.name,
    });
    if (conflict) {
      showEnablementConflict(conflict);
      return;
    }
    try {
      await adminAPI.payment.updateProvider(provider.id, {
        supported_types: updated,
      } as Partial<ProviderInstance>);
      await loadProviders();
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
    }
  }

  function confirmDeleteProvider(provider: ProviderInstance): void {
    deletingProviderId.value = provider.id;
    showDeleteProviderDialog.value = true;
  }

  async function handleReorderProviders(
    updates: Array<{ id: number; sort_order: number }>,
  ): Promise<void> {
    try {
      await Promise.all(
        updates.map((update) =>
          adminAPI.payment.updateProvider(update.id, {
            sort_order: update.sort_order,
          } as Partial<ProviderInstance>),
        ),
      );
      await loadProviders();
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
      void loadProviders();
    }
  }

  async function handleDeleteProvider(): Promise<void> {
    if (!deletingProviderId.value) return;
    try {
      await adminAPI.payment.deleteProvider(deletingProviderId.value);
      showSuccess(t("common.deleted"));
      showDeleteProviderDialog.value = false;
      await loadProviders();
    } catch (error: unknown) {
      showError(
        extractI18nErrorMessage(
          error,
          t,
          "payment.errors",
          t("common.error"),
        ),
      );
    }
  }

  return {
    allPaymentTypes,
    cancelRateLimitModeOptions,
    cancelRateLimitUnitOptions,
    editingProvider,
    enabledProviderKeyOptions,
    handleDeleteProvider,
    handleReorderProviders,
    handleSaveProvider,
    handleToggleField,
    handleToggleType,
    hasAnyPaymentTypeEnabled,
    isPaymentTypeEnabled,
    loadBalanceOptions,
    loadProviders,
    openCreateProvider,
    openEditProvider,
    providerDialogRef,
    providerKeyOptions,
    providerSaving,
    providers,
    providersLoading,
    confirmDeleteProvider,
    showDeleteProviderDialog,
    showProviderDialog,
    togglePaymentType,
  };
}
