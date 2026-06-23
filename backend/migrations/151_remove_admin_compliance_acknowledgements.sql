DELETE FROM settings
WHERE key = 'admin_compliance_acknowledgement'
   OR key LIKE 'admin_compliance_acknowledgement:%';
