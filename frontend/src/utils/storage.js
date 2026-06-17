// Simple obfuscation for client-side storage
// Note: This is not "military-grade" encryption, but prevents casual tampering via dev tools.

const KEY_PREFIX = 'dl_';

export const secureStorage = {
  setItem: (key, value) => {
    const stringValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
    const obfuscatedValue = btoa(encodeURIComponent(stringValue));
    localStorage.setItem(KEY_PREFIX + key, obfuscatedValue);
  },
  
  getItem: (key) => {
    const value = localStorage.getItem(KEY_PREFIX + key);
    if (!value) return null;
    try {
      const decodedValue = decodeURIComponent(atob(value));
      if (decodedValue === 'true') return true;
      if (decodedValue === 'false') return false;
      try {
        return JSON.parse(decodedValue);
      } catch {
        return decodedValue;
      }
    } catch {
      return null;
    }
  },
  
  removeItem: (key) => {
    localStorage.removeItem(KEY_PREFIX + key);
  },
  
  clear: () => {
    Object.keys(localStorage).forEach(k => {
      if (k.startsWith(KEY_PREFIX)) localStorage.removeItem(k);
    });
  }
};

export const parseJwt = (token) => {
  if (!token) return null;
  try {
    const base64Url = token.split('.')[1];
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(atob(base64).split('').map(function(c) {
      return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));
    return JSON.parse(jsonPayload);
  } catch (e) {
    return null;
  }
};
