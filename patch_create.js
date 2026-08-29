const fs = require('fs');
let content = fs.readFileSync('frontend/src/components/UserManager.vue', 'utf8');

content = content.replace(/const createUser = async \(\) => \{/g, `const createUser = async () => {
  isProcessing.value = true;`);

content = content.replace(/const updatePermissions = async \(\) => \{/g, `const updatePermissions = async () => {
  isProcessing.value = true;`);

// Find the end of createUser block and add isProcessing.value = false
content = content.replace(/showToast\("Error", "A network error occurred", "error"\);\n  \}\n\};\n/g, `showToast("Error", "A network error occurred", "error");\n  } finally {\n    isProcessing.value = false;\n  }\n};\n`);

// Also do it for updatePermissions
content = content.replace(/showToast\("Error", "Failed to update permissions", "error"\);\n  \}\n\};\n/g, `showToast("Error", "Failed to update permissions", "error");\n  } finally {\n    isProcessing.value = false;\n  }\n};\n`);

fs.writeFileSync('frontend/src/components/UserManager.vue', content);
