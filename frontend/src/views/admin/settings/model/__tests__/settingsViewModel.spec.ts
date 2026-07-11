import { describe, expect, it } from "vitest";
import {
  findDuplicateDefaultSubscription,
  normalizeLoginAgreementDocumentId,
  parseTablePageSizeOptionsInput,
  resolveNextSettingsTab,
} from "../settingsViewModel";

describe("settingsViewModel", () => {
  it("resolves keyboard navigation without DOM state", () => {
    expect(resolveNextSettingsTab("general", "ArrowLeft")).toBe("backup");
    expect(resolveNextSettingsTab("general", "End")).toBe("backup");
    expect(resolveNextSettingsTab("backup", "Home")).toBe("general");
    expect(resolveNextSettingsTab("general", "Enter")).toBeNull();
  });

  it("normalizes agreement document IDs", () => {
    expect(normalizeLoginAgreementDocumentId(" Terms / Privacy ")).toBe(
      "terms-privacy",
    );
  });

  it("parses, sorts, and deduplicates table page sizes", () => {
    expect(parseTablePageSizeOptionsInput("50, 10, 50, 20")).toEqual([
      10, 20, 50,
    ]);
    expect(parseTablePageSizeOptionsInput("4, 20")).toBeNull();
    expect(parseTablePageSizeOptionsInput("20.5")).toBeNull();
  });

  it("finds duplicate default subscriptions", () => {
    const duplicate = findDuplicateDefaultSubscription([
      { group_id: 3, validity_days: 30 },
      { group_id: 3, validity_days: 90 },
    ]);
    expect(duplicate?.group_id).toBe(3);
  });
});
