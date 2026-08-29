const fs = require('fs');
let content = fs.readFileSync('frontend/src/components/UserManager.vue', 'utf8');

content = content.replace(/const createUser = async \(\) => \{\n  isProcessing\.value = true;\n  if \(\!newUser\.value\.is_admin/g, 
`const createUser = async () => {
  if (!newUser.value.is_admin && !newUser.value.role_template_id && !newUser.value.team_id) return;
  if (newUser.value.authMethod === 'invite' && !newUser.value.email) return;
  if (newUser.value.authMethod === 'local' && (!newUser.value.username || !newUser.value.password)) return;
  isProcessing.value = true;
  if (false`);

fs.writeFileSync('frontend/src/components/UserManager.vue', content);
