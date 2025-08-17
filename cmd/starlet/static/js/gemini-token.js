document.addEventListener('DOMContentLoaded', () => {
  const copyButton = document.getElementById('copy-btn');

  if (copyButton) {
    copyButton.addEventListener('click', copyToken);
  }
});

function copyToken(event) {
  const tokenElement = document.getElementById('jwt-token');
  const copyButton = event.currentTarget;

  const tokenText = tokenElement.textContent;
  navigator.clipboard.writeText(tokenText).then(() => {
    const originalText = copyButton.textContent;
    copyButton.textContent = 'Copied!';
    copyButton.disabled = true;
    setTimeout(() => {
      copyButton.textContent = originalText;
      copyButton.disabled = false;
    }, 2000);
  }).catch(err => {
    console.error('Failed to copy token: ', err);
    copyButton.textContent = 'Error';
  });
}
