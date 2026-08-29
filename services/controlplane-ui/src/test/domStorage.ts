function memoryStorage(): Storage {
  const data = new Map<string, string>();
  return {
    get length() {
      return data.size;
    },
    clear() {
      data.clear();
    },
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null;
    },
    setItem(key: string, value: string) {
      data.set(String(key), String(value));
    },
    removeItem(key: string) {
      data.delete(String(key));
    },
    key(index: number) {
      return [...data.keys()][index] ?? null;
    }
  };
}

export function resetDomStorage(): void {
  const local = memoryStorage();
  const session = memoryStorage();
  Object.defineProperty(window, "localStorage", { configurable: true, value: local });
  Object.defineProperty(window, "sessionStorage", { configurable: true, value: session });
}
