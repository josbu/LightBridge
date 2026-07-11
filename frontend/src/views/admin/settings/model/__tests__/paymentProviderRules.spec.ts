import { describe, expect, it } from "vitest";
import {
  findProviderEnablementConflict,
  getProviderVisibleMethods,
} from "../paymentProviderRules";
import type { ProviderInstance } from "@/types/payment";

function provider(
  overrides: Partial<ProviderInstance>,
): ProviderInstance {
  return {
    id: 1,
    provider_key: "easypay",
    name: "provider",
    enabled: true,
    supported_types: [],
    ...overrides,
  } as ProviderInstance;
}

describe("paymentProviderRules", () => {
  it("maps provider configuration to user-visible methods", () => {
    expect(
      getProviderVisibleMethods(
        provider({ provider_key: "easypay", supported_types: ["alipay"] }),
      ),
    ).toEqual(["alipay"]);
    expect(
      getProviderVisibleMethods(
        provider({ provider_key: "wxpay", supported_types: [] }),
      ),
    ).toEqual(["wxpay"]);
  });

  it("detects conflicts only between enabled providers", () => {
    const existing = provider({ id: 7, provider_key: "alipay" });
    const candidate = provider({ id: 8, provider_key: "easypay", supported_types: ["alipay"] });
    expect(findProviderEnablementConflict(candidate, [existing])?.method).toBe(
      "alipay",
    );
    expect(
      findProviderEnablementConflict(
        { ...candidate, enabled: false },
        [existing],
      ),
    ).toBeNull();
  });
});
