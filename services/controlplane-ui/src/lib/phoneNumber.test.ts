import { describe, expect, it } from "vitest";
import { DEFAULT_PHONE_COUNTRY, PHONE_COUNTRIES, nationalDigits } from "./phoneNumber";

describe("national phone digits", () => {
  it("keeps every supported country at 10 national digits or fewer", () => {
    expect(Math.max(...PHONE_COUNTRIES.map((country) => country.maxDigits))).toBe(10);
    for (const country of PHONE_COUNTRIES) {
      expect(country.example).toHaveLength(country.maxDigits);
      expect(country.example).toMatch(/^\d+$/);
    }
  });

  it("keeps an Indian mobile number as one continuous run", () => {
    expect(DEFAULT_PHONE_COUNTRY.maxDigits).toBe(10);
    expect(nationalDigits("98765 43210", 10)).toBe("9876543210");
    expect(nationalDigits("9876543210999", 10)).toBe("9876543210");
  });

  it("drops letters and extra digits", () => {
    expect(nationalDigits("98-765 43210 extra", 10)).toBe("9876543210");
    expect(nationalDigits("1234567", 6)).toBe("123456");
  });
});
