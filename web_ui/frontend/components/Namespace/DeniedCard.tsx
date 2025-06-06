import React, { useContext, useRef, useState } from 'react';
import { green, red } from '@mui/material/colors';
import { Avatar, Box, IconButton, Tooltip, Typography } from '@mui/material';
import { Check, Delete, Person } from '@mui/icons-material';
import { Alert, RegistryNamespace, User } from '@/index';
import InformationDropdown from './InformationDropdown';
import { NamespaceIcon } from '@/components/Namespace/index';
import { AlertContext, AlertDispatchContext } from '@/components/AlertProvider';
import { useSWRConfig } from 'swr';
import {
  approveNamespace,
  deleteNamespace,
  NAMESPACE_KEY,
} from '@/helpers/api';
import { alertOnError } from '@/helpers/util';

export interface DeniedCardProps {
  namespace: RegistryNamespace;
  onUpdate: () => void;
  onAlert: (alert: Alert) => void;
  authenticated?: User;
}

export const DeniedCard = ({ namespace, authenticated }: DeniedCardProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const [transition, setTransition] = useState<boolean>(false);
  const dispatch = useContext(AlertDispatchContext);
  const alert = useContext(AlertContext);
  const { mutate } = useSWRConfig();

  return (
    <>
      <Box>
        <Box
          sx={{
            cursor: 'pointer',
            display: 'flex',
            width: '100%',
            justifyContent: 'space-between',
            border: 'solid #ececec 1px',
            borderRadius: transition ? '10px 10px 0px 0px' : 2,
            transition: 'background-color .3s ease-out',
            bgcolor:
              alert?.alertProps?.severity == 'success'
                ? green[100]
                : alert?.alertProps?.severity == 'error'
                  ? red[100]
                  : 'inherit',
            '&:hover': {
              bgcolor: alert ? undefined : '#ececec',
            },
            p: 1,
          }}
          bgcolor={'secondary'}
          onClick={() => setTransition(!transition)}
        >
          <Box my={'auto'} ml={1} display={'flex'} flexDirection={'row'}>
            <NamespaceIcon serverType={namespace.type} />
            <Typography sx={{ pt: '2px' }}>{namespace.prefix}</Typography>
          </Box>
          <Box display={'flex'}>
            <Box my={'auto'} display={'flex'} flexDirection={'row'}>
              {authenticated !== undefined &&
                authenticated.user == namespace.admin_metadata.user_id && (
                  <Box sx={{ borderRight: 'solid 1px #ececec', mr: 1 }}>
                    <Tooltip title={'Created By You'}>
                      <Avatar
                        sx={{
                          height: '40px',
                          width: '40px',
                          my: 'auto',
                          mr: 2,
                        }}
                      >
                        <Person />
                      </Avatar>
                    </Tooltip>
                  </Box>
                )}
              {authenticated?.role == 'admin' && (
                <>
                  <Tooltip title={'Delete Registration'}>
                    <IconButton
                      sx={{ bgcolor: '#ff00001a', mx: 1 }}
                      color={'error'}
                      onClick={async (e) => {
                        e.stopPropagation();
                        await alertOnError(
                          () => deleteNamespace(namespace.id),
                          'Could Not Delete Registration',
                          dispatch
                        );
                        mutate(NAMESPACE_KEY);
                      }}
                    >
                      <Delete />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title={'Approve Registration'}>
                    <IconButton
                      sx={{ bgcolor: '#2e7d3224', mx: 1 }}
                      color={'success'}
                      onClick={async (e) => {
                        e.stopPropagation();
                        await alertOnError(
                          () => approveNamespace(namespace.id),
                          'Could Not Approve Registration',
                          dispatch
                        );
                        mutate(NAMESPACE_KEY);
                      }}
                    >
                      <Check />
                    </IconButton>
                  </Tooltip>
                </>
              )}
            </Box>
          </Box>
        </Box>
        <Box ref={ref}>
          <InformationDropdown
            adminMetadata={namespace.admin_metadata}
            transition={transition}
            parentRef={ref}
          />
        </Box>
      </Box>
    </>
  );
};

export default DeniedCard;
