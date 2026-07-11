import { normalizeVisibleMethod } from "@/components/payment/paymentFlow";
import type { ProviderInstance } from "@/types/payment";

export type PaymentVisibleMethod = "alipay" | "wxpay";

export type ProviderEnablementCandidate = Pick<
  ProviderInstance,
  "id" | "provider_key" | "supported_types" | "enabled" | "name"
>;

export interface ProviderEnablementConflict {
  method: PaymentVisibleMethod;
  conflicting: ProviderInstance;
}

export function getProviderVisibleMethods(
  provider: ProviderEnablementCandidate,
): PaymentVisibleMethod[] {
  if (!provider.enabled) {
    return [];
  }

  const supportedTypes = Array.isArray(provider.supported_types)
    ? provider.supported_types
    : [];
  const methods = new Set<PaymentVisibleMethod>();
  const addMethod = (type: string) => {
    const method = normalizeVisibleMethod(type);
    if (method === "alipay" || method === "wxpay") {
      methods.add(method);
    }
  };

  if (provider.provider_key === "alipay") {
    if (supportedTypes.length === 0) {
      methods.add("alipay");
    } else {
      supportedTypes.forEach((type) => {
        if (normalizeVisibleMethod(type) === "alipay") {
          methods.add("alipay");
        }
      });
    }
  } else if (provider.provider_key === "wxpay") {
    if (supportedTypes.length === 0) {
      methods.add("wxpay");
    } else {
      supportedTypes.forEach((type) => {
        if (normalizeVisibleMethod(type) === "wxpay") {
          methods.add("wxpay");
        }
      });
    }
  } else if (provider.provider_key === "easypay") {
    supportedTypes.forEach(addMethod);
  }

  return Array.from(methods);
}

export function findProviderEnablementConflict(
  candidate: ProviderEnablementCandidate,
  providers: ProviderInstance[],
): ProviderEnablementConflict | null {
  const claimedMethods = getProviderVisibleMethods(candidate);
  if (claimedMethods.length === 0) {
    return null;
  }

  for (const other of providers) {
    if (other.id === candidate.id || !other.enabled) {
      continue;
    }

    const otherMethods = getProviderVisibleMethods(other);
    const matchedMethod = claimedMethods.find((method) =>
      otherMethods.includes(method),
    );
    if (matchedMethod) {
      return { method: matchedMethod, conflicting: other };
    }
  }

  return null;
}
