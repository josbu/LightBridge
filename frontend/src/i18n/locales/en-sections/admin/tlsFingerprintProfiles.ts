    // TLS Fingerprint Profiles
export default {
      title: 'TLS Fingerprint Profiles',
      description: 'Manage TLS fingerprint profiles for simulating specific client TLS handshake characteristics',
      createProfile: 'Create Profile',
      editProfile: 'Edit Profile',
      deleteProfile: 'Delete Profile',
      noProfiles: 'No profiles configured',
      createFirstProfile: 'Create your first TLS fingerprint profile',

      columns: {
        name: 'Name',
        description: 'Description',
        grease: 'GREASE',
        alpn: 'ALPN',
        actions: 'Actions'
      },

      form: {
        pasteYaml: 'Paste YAML Configuration',
        pasteYamlPlaceholder: 'Paste YAML output from TLS Fingerprint Collector here...',
        pasteYamlHint: 'Paste the YAML copied from TLS Fingerprint Collector to auto-fill all fields.',
        openCollector: 'Open Collector',
        parseYaml: 'Parse YAML',
        yamlParsed: 'YAML parsed successfully, fields auto-filled',
        yamlParseFailed: 'Failed to parse YAML: name field not found',
        name: 'Profile Name',
        namePlaceholder: 'e.g. macOS Node.js v24',
        description: 'Description',
        descriptionPlaceholder: 'Optional description for this profile',
        enableGrease: 'Enable GREASE',
        enableGreaseHint: 'Insert GREASE values in TLS ClientHello extensions',
        cipherSuites: 'Cipher Suites',
        cipherSuitesHint: 'Comma-separated hex values, e.g. 0x1301, 0x1302, 0xc02c',
        curves: 'Elliptic Curves',
        curvesHint: 'Comma-separated curve IDs',
        pointFormats: 'Point Formats',
        signatureAlgorithms: 'Signature Algorithms',
        alpnProtocols: 'ALPN Protocols',
        alpnProtocolsHint: 'Comma-separated, e.g. h2, http/1.1',
        supportedVersions: 'Supported TLS Versions',
        keyShareGroups: 'Key Share Groups',
        pskModes: 'PSK Modes',
        extensions: 'Extensions'
      },

      deleteConfirm: 'Delete Profile',
      deleteConfirmMessage: 'Are you sure you want to delete profile "{name}"? Accounts using this profile will fall back to the built-in default.',
      createSuccess: 'Profile created successfully',
      updateSuccess: 'Profile updated successfully',
      deleteSuccess: 'Profile deleted successfully',
      loadFailed: 'Failed to load profiles',
      saveFailed: 'Failed to save profile',
      deleteFailed: 'Failed to delete profile'
    }
