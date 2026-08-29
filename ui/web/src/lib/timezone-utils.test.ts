import { describe, expect, it } from "vitest";
import { getAllIanaTimezones, isValidIanaTimezone, normalizeIanaTimezone } from "./timezone-utils";

describe("timezone aliases", () => {
  it("uses Asia/Ho_Chi_Minh as the persisted Vietnam timezone", () => {
    expect(normalizeIanaTimezone("Asia/Ho_Chi_Minh")).toBe("Asia/Ho_Chi_Minh");
    expect(normalizeIanaTimezone("Asia/Saigon")).toBe("Asia/Ho_Chi_Minh");
  });

  it("accepts both Vietnam timezone identifiers and shows the preferred one", () => {
    expect(isValidIanaTimezone("Asia/Ho_Chi_Minh")).toBe(true);
    expect(isValidIanaTimezone("Asia/Saigon")).toBe(true);
    expect(getAllIanaTimezones().some((timezone) => timezone.value === "Asia/Ho_Chi_Minh")).toBe(true);
  });
});
