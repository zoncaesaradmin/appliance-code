// Digits stay in one run. The field adds the same gap between every digit,
// for every country. The country code is chosen separately.
export const PHONE_COUNTRIES = [
  { code: "IN", name: "India", flag: "🇮🇳", dialCode: "+91", minDigits: 10, maxDigits: 10, example: "9876543210" },
  { code: "US", name: "United States", flag: "🇺🇸", dialCode: "+1", minDigits: 10, maxDigits: 10, example: "2025550147" },
  { code: "GB", name: "United Kingdom", flag: "🇬🇧", dialCode: "+44", minDigits: 10, maxDigits: 10, example: "7123456789" },
  { code: "AE", name: "United Arab Emirates", flag: "🇦🇪", dialCode: "+971", minDigits: 9, maxDigits: 9, example: "501234567" },
  { code: "SG", name: "Singapore", flag: "🇸🇬", dialCode: "+65", minDigits: 8, maxDigits: 8, example: "91234567" },
  { code: "AU", name: "Australia", flag: "🇦🇺", dialCode: "+61", minDigits: 9, maxDigits: 9, example: "412345678" }
] as const;

export type PhoneCountry = (typeof PHONE_COUNTRIES)[number];

export const DEFAULT_PHONE_COUNTRY = PHONE_COUNTRIES[0];

export const VERIFICATION_CODE_LENGTH = 6;

export function nationalDigits(value: string, maxDigits: number): string {
  return value.replace(/\D/g, "").slice(0, maxDigits);
}
